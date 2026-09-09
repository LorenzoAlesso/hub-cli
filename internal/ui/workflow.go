package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	tea "charm.land/bubbletea/v2"
)

// ── States ────────────────────────────────────────────────────────────────────

type wfState int

const (
	wfECRLogin wfState = iota
	wfBranchLoading
	wfBranchSelect
	wfRepoPrep
	wfDockerBranchLoading
	wfDockerBranchSelect
	wfDockerRepoPrep
	wfServiceSelect
	wfSvcTagSync
	wfSvcDockerfile
	wfSvcDockerfileMissing
	wfSvcTagInput
	wfSvcBuildArg
	wfSvcBuilding
	wfSvcPushing
	wfSvcHelm
	wfSvcHelmError
	wfSvcRollback
	wfPostSync // once per workflow, not once per service
	wfSummary
)

// ── Messages ──────────────────────────────────────────────────────────────────

type wfOpDoneMsg struct {
	err    error
	output []byte
}

type wfBranchesLoadedMsg struct {
	branches []string
	err      error
}

type wfDockerBranchesLoadedMsg struct {
	branches []string
	err      error
}

type wfDockerRepoPrepDoneMsg struct {
	dir string
	err error
}

type wfRepoPrepDoneMsg struct {
	dir string
	err error
}

type wfPostSyncDoneMsg struct {
	err   error
	lines []string
}

// ── Model ─────────────────────────────────────────────────────────────────────

type WorkflowModel struct {
	cfg    *config.Config
	dryRun bool
	testUI bool

	state     wfState
	width     int
	cancelled bool

	log []string // rendered history lines

	spinner  spinnerModel
	multisel multiSelectModel
	input    inputModel
	list     listModel
	confirm  confirmModel

	opStart time.Time

	selectedServices []string
	svcIdx           int

	svcName        string
	svc            config.ServiceConfig
	oldTag         string
	newTag         string
	suggestedTag   string
	dockerfilePath string
	discovered     bool
	buildArgs      map[string]string
	buildArgQueue  []logic.DockerArg
	buildArgIdx    int
	svcStart       time.Time
	helmSetArg     string
	valuesPath     string
	chartVersion   string

	localSources  bool              // values and Dockerfile from the working copies: test deploy, sync skipped
	helmRemoteURL string            // origin of the charts repo, read from the working copy
	helmBranch    string            // chart branch chosen for this run
	helmRepoDir   string            // managed clone the values are read from, "" = working copy
	dockerURL     string            // origin of the Docker repo, read from the working copy
	dockerBranch  string            // Docker branch chosen for this run
	dockerRepoDir string            // managed clone the build reads from, "" = working copy
	deployed      []deployedService // services that reached the cluster, for the final commit
	syncErr       error

	results []DeployResult
}

// deployedService remembers what a finished service needs for the final sync.
type deployedService struct {
	name string
	tag  string
	svc  config.ServiceConfig
}

// RunWorkflow runs the full deploy pipeline as a single persistent BubbleTea program.
// Returns (results, cancelled, syncErr, error): syncErr is not fatal, the deploy
// already reached the cluster, but the repository no longer reflects it.
func RunWorkflow(cfg *config.Config, dryRun, testUI, localSources bool) ([]DeployResult, bool, error, error) {
	label := "LOCAL DEPLOY"
	switch {
	case testUI:
		label = "TEST-UI"
	case localSources:
		label = "LOCAL DEPLOY (prova)"
	}
	SetStatus(label, cfg.Config.ECRRegion)

	m := WorkflowModel{
		cfg:          cfg,
		dryRun:       dryRun,
		testUI:       testUI,
		localSources: localSources,
		state:        wfECRLogin,
		spinner:      newSpinnerModel("ECR Login"),
		opStart:      time.Now(),
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	ClearStatus()
	if err != nil {
		return nil, false, nil, err
	}
	wf := final.(WorkflowModel)
	return wf.results, wf.cancelled, wf.syncErr, nil
}

// helmValuesRoot is the directory the values file is read from: the managed
// clone, or the working copy under --local-sources.
func (m WorkflowModel) helmValuesRoot() string {
	if m.helmRepoDir != "" {
		return m.helmRepoDir
	}
	return m.cfg.Config.HelmRootPath
}

// dockerBuildRoot is the directory the build reads Dockerfiles from. Like the
// chart repo, the Docker repo carries one branch per site and environment, so
// what gets baked into the image depends on the branch — not on how the working
// copy happens to be left.
func (m WorkflowModel) dockerBuildRoot() string {
	if m.dockerRepoDir != "" {
		return m.dockerRepoDir
	}
	return m.cfg.Config.DockerRootPath
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m WorkflowModel) Init() tea.Cmd {
	if m.dryRun {
		return func() tea.Msg { return wfOpDoneMsg{} }
	}
	cfg := m.cfg
	testUI := m.testUI
	return tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(900 * time.Millisecond)
			return wfOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.ECRLogin(cfg.Config.ECRRegion, cfg.Config.ECRAccountID, &buf)
		return wfOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m WorkflowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}
	if wm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wm.Width
		return m, nil
	}
	if done, ok := msg.(wfOpDoneMsg); ok {
		return m.handleOpDone(done)
	}
	if bl, ok := msg.(wfBranchesLoadedMsg); ok {
		if bl.err != nil {
			m.log = append(m.log, ErrStyle.Render("  ✗  "+bl.err.Error()))
			m.log = append(m.log, DimStyle.Render(
				"      Per usare le copie di lavoro: hub-cli local --local-sources"))
			m.cancelled = true
			return m, tea.Quit
		}
		return m.enterBranchSelect(bl.branches)
	}
	if rp, ok := msg.(wfRepoPrepDoneMsg); ok {
		if rp.err != nil {
			// A stale or wrong-branch values file would silently revert
			// somebody else's change on the cluster.
			m.log = append(m.log, ErrStyle.Render("  ✗  Allineamento del repo charts non riuscito: "+rp.err.Error()))
			m.log = append(m.log, DimStyle.Render(
				"      Per usare le copie di lavoro: hub-cli local --local-sources"))
			m.cancelled = true
			return m, tea.Quit
		}
		m.helmRepoDir = rp.dir
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render(
			"  Values da origin/"+m.helmBranch+"  ")+
			ValueStyle.Render(formatElapsed(time.Since(m.opStart))))
		return m.enterDockerBranchLoading()
	}
	if bl, ok := msg.(wfDockerBranchesLoadedMsg); ok {
		if bl.err != nil {
			m.log = append(m.log, ErrStyle.Render("  ✗  "+bl.err.Error()))
			m.cancelled = true
			return m, tea.Quit
		}
		return m.enterDockerBranchSelect(bl.branches)
	}
	if rp, ok := msg.(wfDockerRepoPrepDoneMsg); ok {
		if rp.err != nil {
			m.log = append(m.log, ErrStyle.Render(
				"  ✗  Allineamento del repo Docker non riuscito: "+rp.err.Error()))
			m.cancelled = true
			return m, tea.Quit
		}
		m.dockerRepoDir = rp.dir
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render(
			"  Dockerfile da origin/"+m.dockerBranch+"  ")+
			ValueStyle.Render(formatElapsed(time.Since(m.opStart))))
		return m.enterServiceSelect()
	}
	if ps, ok := msg.(wfPostSyncDoneMsg); ok {
		m.log = append(m.log, ps.lines...)
		m.syncErr = ps.err
		return m.enterSummary()
	}
	return m.forwardToActive(msg)
}

func (m WorkflowModel) forwardToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case wfECRLogin, wfSvcBuilding, wfSvcPushing, wfSvcHelm, wfSvcRollback,
		wfBranchLoading, wfRepoPrep, wfDockerBranchLoading, wfDockerRepoPrep, wfPostSync:
		sm, cmd := m.spinner.Update(msg)
		m.spinner = sm.(spinnerModel)
		return m, cmd

	case wfServiceSelect:
		sm, cmd := m.multisel.Update(msg)
		m.multisel = sm.(multiSelectModel)
		if m.multisel.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.multisel.done {
			return m.finishServiceSelect()
		}
		return m, cmd

	case wfSvcTagSync:
		sm, cmd := m.confirm.Update(msg)
		m.confirm = sm.(confirmModel)
		if m.confirm.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.confirm.done {
			return m.finishTagSync()
		}
		return m, cmd

	case wfBranchSelect, wfDockerBranchSelect, wfSvcDockerfile, wfSvcDockerfileMissing, wfSvcHelmError:
		sm, cmd := m.list.Update(msg)
		m.list = sm.(listModel)
		if m.list.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.list.done {
			switch m.state {
			case wfBranchSelect:
				return m.finishBranchSelect()
			case wfDockerBranchSelect:
				return m.finishDockerBranchSelect()
			case wfSvcDockerfile:
				return m.finishDockerfile()
			case wfSvcDockerfileMissing:
				return m.finishDockerfileMissing()
			}
			return m.finishHelmError()
		}
		return m, cmd

	case wfSvcTagInput, wfSvcBuildArg:
		sm, cmd := m.input.Update(msg)
		m.input = sm.(inputModel)
		if m.input.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.input.done {
			if m.state == wfSvcTagInput {
				return m.finishTagInput()
			}
			return m.finishBuildArg()
		}
		return m, cmd
	}
	return m, nil
}

// ── handleOpDone ──────────────────────────────────────────────────────────────

func (m WorkflowModel) handleOpDone(msg wfOpDoneMsg) (tea.Model, tea.Cmd) {
	elapsed := formatElapsed(time.Since(m.opStart))

	if msg.err != nil {
		switch m.state {
		case wfECRLogin:
			m.log = append(m.log, ErrStyle.Render("  ✗  ECR Login — "+msg.err.Error()))
			if len(msg.output) > 0 {
				m.log = append(m.log, DimStyle.Render(string(msg.output)))
			}
			return m, tea.Quit
		case wfSvcBuilding:
			m.log = append(m.log, ErrStyle.Render("  ✗  Docker Build fallito"))
			if len(msg.output) > 0 {
				m.log = append(m.log, DimStyle.Render(string(msg.output)))
			}
			return m, tea.Quit
		case wfSvcPushing:
			m.log = append(m.log, ErrStyle.Render("  ✗  Docker Push fallito"))
			return m, tea.Quit
		case wfSvcHelm:
			m.log = append(m.log, ErrStyle.Render("  ✗  Helm Upgrade fallito: "+msg.err.Error()))
			return m.enterHelmError()
		case wfSvcRollback:
			m.log = append(m.log, ErrStyle.Render("  ✗  Rollback fallito: "+msg.err.Error()))
			m.cancelled = true
			return m, tea.Quit
		}
	}

	switch m.state {
	case wfECRLogin:
		if m.dryRun {
			m.log = append(m.log, wfDryRunLine(fmt.Sprintf(
				"aws ecr get-login-password --region %s | docker login --username AWS %s.dkr.ecr.%s.amazonaws.com",
				m.cfg.Config.ECRRegion, m.cfg.Config.ECRAccountID, m.cfg.Config.ECRRegion)))
		}
		return m.enterBranchLoading()

	case wfSvcBuilding:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Build  ")+ValueStyle.Render(elapsed))
		return m.enterPush()

	case wfSvcPushing:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Push  ")+ValueStyle.Render(elapsed))
		return m.enterHelm()

	case wfSvcHelm:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Deploy  ")+ValueStyle.Render(elapsed))
		// Save tag synchronously (fast file write)
		if err := config.UpdateServiceTag(m.svcName, m.newTag); err != nil {
			m.log = append(m.log, WarnStyle.Render("  ⚠  impossibile aggiornare last_tag: "+err.Error()))
		}
		m.deployed = append(m.deployed, deployedService{name: m.svcName, tag: m.newTag, svc: m.svc})
		return m.finishService()

	case wfSvcRollback:
		m.log = append(m.log, WarnStyle.Render("  ⚠  Rollback completato"))
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// ── Chart branch and managed repo prep ────────────────────────────────────────

// enterBranchLoading reads the branches published on the charts remote. Which
// one holds the right values depends on the site being worked on, so it is a
// per-run choice rather than a setting.
func (m WorkflowModel) enterBranchLoading() (tea.Model, tea.Cmd) {
	helmRoot := m.cfg.Config.HelmRootPath

	switch {
	case m.localSources:
		m.log = append(m.log, WarnStyle.Render(
			"  ⚠  Deploy di prova: values e Dockerfile dalle copie di lavoro, sync disattivato"))
		return m.enterServiceSelect()
	case m.dryRun || m.testUI:
		if m.dryRun {
			m.log = append(m.log, wfDryRunLine("selezione del branch chart e allineamento del repo gestito"))
		}
		return m.enterServiceSelect()
	case helmRoot == "":
		return m.enterServiceSelect()
	}

	remoteURL, err := logic.GitRemoteURL(helmRoot)
	if err != nil {
		return m, func() tea.Msg { return wfBranchesLoadedMsg{err: err} }
	}
	m.helmRemoteURL = remoteURL

	m.state = wfBranchLoading
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Lettura branch dei chart")
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		branches, err := logic.ListRemoteBranches(remoteURL)
		return wfBranchesLoadedMsg{branches: branches, err: err}
	})
}

// enterBranchSelect asks which branch the values come from, preselecting the one
// used last so the common case stays a single Enter.
func (m WorkflowModel) enterBranchSelect(branches []string) (tea.Model, tea.Cmd) {
	if len(branches) == 0 {
		m.log = append(m.log, ErrStyle.Render("  ✗  Nessun branch pubblicato su "+m.helmRemoteURL))
		m.cancelled = true
		return m, tea.Quit
	}
	if len(branches) == 1 {
		return m.enterRepoPrep(branches[0])
	}

	workingCopy, _ := logic.GitCurrentBranch(m.cfg.Config.HelmRootPath)
	items := make([]Item, len(branches))
	for i, b := range branches {
		items[i] = Item{Value: b, Label: b}
		if b == workingCopy {
			items[i].Desc = "branch della copia di lavoro"
		}
	}

	m.state = wfBranchSelect
	m.list = listModel{
		title:  "Branch dei chart da cui leggere i values",
		items:  items,
		cursor: wfDefaultBranchIndex(branches, config.GetHelmSyncBranch(), workingCopy),
		width:  m.width,
	}
	return m, m.list.Init()
}

func (m WorkflowModel) finishBranchSelect() (tea.Model, tea.Cmd) {
	return m.enterRepoPrep(m.list.selected)
}

// wfDefaultBranchIndex preselects the branch used last, falling back to the one
// the working copy is on.
func wfDefaultBranchIndex(branches []string, lastUsed, workingCopy string) int {
	for _, want := range []string{lastUsed, workingCopy} {
		if want == "" {
			continue
		}
		for i, b := range branches {
			if b == want {
				return i
			}
		}
	}
	return 0
}

// enterRepoPrep realigns the managed clone to the chosen branch before anything
// is deployed: the values file passed to `helm -f` is applied to the cluster, so
// it must come from a known, freshly fetched state.
func (m WorkflowModel) enterRepoPrep(branch string) (tea.Model, tea.Cmd) {
	m.helmBranch = branch
	if err := config.SetHelmSyncBranch(branch); err != nil {
		m.log = append(m.log, WarnStyle.Render("  ⚠  Branch scelto non salvato: "+err.Error()))
	}

	m.state = wfRepoPrep
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Allineamento repo chart (" + branch + ")")
	remoteURL := m.helmRemoteURL
	reposRoot := config.GetReposRoot()
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		dir, err := logic.EnsureRepo(remoteURL, branch, reposRoot, nil)
		return wfRepoPrepDoneMsg{dir: dir, err: err}
	})
}

// enterDockerBranchLoading reads the branches published on the Docker remote.
func (m WorkflowModel) enterDockerBranchLoading() (tea.Model, tea.Cmd) {
	dockerRoot := m.cfg.Config.DockerRootPath
	if m.localSources || m.dryRun || m.testUI || dockerRoot == "" {
		return m.enterServiceSelect()
	}

	remoteURL, err := logic.GitRemoteURL(dockerRoot)
	if err != nil {
		return m, func() tea.Msg { return wfDockerBranchesLoadedMsg{err: err} }
	}
	m.dockerURL = remoteURL

	m.state = wfDockerBranchLoading
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Lettura branch del repo Docker")
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		branches, err := logic.ListRemoteBranches(remoteURL)
		return wfDockerBranchesLoadedMsg{branches: branches, err: err}
	})
}

func (m WorkflowModel) enterDockerBranchSelect(branches []string) (tea.Model, tea.Cmd) {
	if len(branches) == 0 {
		m.log = append(m.log, ErrStyle.Render("  ✗  Nessun branch pubblicato su "+m.dockerURL))
		m.cancelled = true
		return m, tea.Quit
	}
	if len(branches) == 1 {
		return m.enterDockerRepoPrep(branches[0])
	}

	workingCopy, _ := logic.GitCurrentBranch(m.cfg.Config.DockerRootPath)
	items := make([]Item, len(branches))
	for i, b := range branches {
		items[i] = Item{Value: b, Label: b}
		if b == workingCopy {
			items[i].Desc = "branch della copia di lavoro"
		}
	}

	m.state = wfDockerBranchSelect
	m.list = listModel{
		title:  "Branch del repo Docker da cui buildare",
		items:  items,
		cursor: wfDefaultBranchIndex(branches, config.GetDockerBranch(), workingCopy),
		width:  m.width,
	}
	return m, m.list.Init()
}

func (m WorkflowModel) finishDockerBranchSelect() (tea.Model, tea.Cmd) {
	return m.enterDockerRepoPrep(m.list.selected)
}

func (m WorkflowModel) enterDockerRepoPrep(branch string) (tea.Model, tea.Cmd) {
	m.dockerBranch = branch
	if err := config.SetDockerBranch(branch); err != nil {
		m.log = append(m.log, WarnStyle.Render("  ⚠  Branch Docker scelto non salvato: "+err.Error()))
	}

	m.state = wfDockerRepoPrep
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Allineamento repo Docker (" + branch + ")")
	remoteURL := m.dockerURL
	reposRoot := config.GetReposRoot()
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		dir, err := logic.EnsureRepo(remoteURL, branch, reposRoot, nil)
		return wfDockerRepoPrepDoneMsg{dir: dir, err: err}
	})
}

// ── enterServiceSelect ────────────────────────────────────────────────────────

func (m WorkflowModel) enterServiceSelect() (tea.Model, tea.Cmd) {
	m.state = wfServiceSelect
	names := wfSortedServiceKeys(m.cfg.Services)
	items := make([]Item, len(names))
	for i, name := range names {
		items[i] = Item{Value: name, Label: name, Desc: m.cfg.Services[name].LastTag}
	}
	m.multisel = multiSelectModel{
		title:    "Servizi da deployare",
		items:    items,
		selected: make(map[int]bool),
		width:    m.width,
	}
	return m, m.multisel.Init()
}

func (m WorkflowModel) finishServiceSelect() (tea.Model, tea.Cmd) {
	selected := make([]string, 0, len(m.multisel.selected))
	for i, item := range m.multisel.items {
		if m.multisel.selected[i] {
			selected = append(selected, item.Value)
		}
	}
	m.selectedServices = selected
	m.svcIdx = 0
	return m.startNextService()
}

// ── startNextService ──────────────────────────────────────────────────────────

func (m WorkflowModel) startNextService() (tea.Model, tea.Cmd) {
	if m.svcIdx >= len(m.selectedServices) {
		return m.enterPostSync()
	}

	m.svcName = m.selectedServices[m.svcIdx]
	m.svc = m.cfg.Services[m.svcName]
	m.svcStart = time.Now()
	m.buildArgs = make(map[string]string)
	m.buildArgQueue = nil
	m.buildArgIdx = 0
	m.oldTag = m.svc.LastTag
	m.newTag = ""
	m.dockerfilePath = ""
	m.discovered = false

	SetStatus(m.svcName, m.cfg.Config.ECRRegion)

	if len(m.selectedServices) > 1 {
		m.log = append(m.log, "\n"+SectionStyle.Render(fmt.Sprintf(
			"── Servizio [%d/%d]: %s", m.svcIdx+1, len(m.selectedServices), strings.ToUpper(m.svcName))))
	}

	// Tag sync (synchronous kubectl get — typically fast)
	if !m.testUI && m.svc.ReleaseName != "" && m.svc.Namespace != "" && m.svc.HelmSetKey != "" {
		if deployedTag, err := logic.GetDeployedTag(m.svc.ReleaseName, m.svc.Namespace, m.svc.HelmSetKey); err == nil {
			if deployedTag != m.svc.LastTag {
				return m.enterTagSync(deployedTag)
			}
		}
	}

	return m.enterDockerfileResolve()
}

// ── Tag sync ──────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterTagSync(deployedTag string) (tea.Model, tea.Cmd) {
	m.state = wfSvcTagSync
	body := fmt.Sprintf("  %s  %s\n  %s  %s",
		LabelStyle.Render("Deployato: "), SuccessStyle.Render(deployedTag),
		LabelStyle.Render("Salvato:   "), WarnStyle.Render(m.svc.LastTag),
	)
	m.confirm = confirmModel{
		title: fmt.Sprintf("Tag sfasato per %q — aggiornare?", m.svcName),
		body:  body,
		width: m.width,
	}
	m.suggestedTag = deployedTag // reuse field to carry deployed tag
	return m, m.confirm.Init()
}

func (m WorkflowModel) finishTagSync() (tea.Model, tea.Cmd) {
	deployedTag := m.suggestedTag
	if m.confirm.choice == 0 {
		if err := config.UpdateServiceTag(m.svcName, deployedTag); err != nil {
			m.log = append(m.log, WarnStyle.Render("  ⚠  impossibile salvare tag sincronizzato: "+err.Error()))
		} else {
			m.svc.LastTag = deployedTag
			m.oldTag = deployedTag
			m.log = append(m.log, SuccessStyle.Render(fmt.Sprintf(`  ✓  last_tag aggiornato a %q`, deployedTag)))
		}
	}
	return m.enterDockerfileResolve()
}

// ── Dockerfile resolve ────────────────────────────────────────────────────────

func (m WorkflowModel) enterDockerfileResolve() (tea.Model, tea.Cmd) {
	svc := m.svc
	buildRoot := m.dockerBuildRoot()

	if svc.DockerfileSubpath != "" {
		full := filepath.Join(buildRoot, svc.DockerfileSubpath)
		abs, err := filepath.Abs(full)
		if err == nil {
			if _, err := os.Stat(abs); err == nil {
				m.dockerfilePath = abs
				m.discovered = false
				m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+abs))
				return m.enterTagInput()
			}
			// A configured path that is missing usually means the repo is on a
			// branch without this service, so scanning is the user's decision.
			m.log = append(m.log, WarnStyle.Render("  ⚠  Dockerfile configurato non trovato: "+abs))
			if branch, err := logic.GitCurrentBranch(buildRoot); err == nil && branch != "" {
				m.log = append(m.log, WarnStyle.Render("      branch corrente del repo Docker: "+branch))
			}
			return m.enterDockerfileMissing()
		}
	}

	return m.enterDockerfileScan()
}

// enterDockerfileMissing asks what to do about a configured Dockerfile that is
// not on disk. Cancelling comes first, so the safe answer is preselected.
func (m WorkflowModel) enterDockerfileMissing() (tea.Model, tea.Cmd) {
	m.state = wfSvcDockerfileMissing
	m.list = listModel{
		title: "Dockerfile di " + m.svcName + " non trovato",
		items: []Item{
			{Value: "cancel", Label: "Annulla  — interrompe il deploy senza buildare"},
			{Value: "scan", Label: "Cerca comunque un Dockerfile", Desc: "l'immagine verrà pushata come " + m.svcName},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m WorkflowModel) finishDockerfileMissing() (tea.Model, tea.Cmd) {
	if m.list.selected != "scan" {
		m.log = append(m.log, ErrStyle.Render("  ✗  Deploy annullato: Dockerfile di "+m.svcName+" non trovato"))
		m.cancelled = true
		return m, tea.Quit
	}
	return m.enterDockerfileScan()
}

// enterDockerfileScan discovers Dockerfiles under the Docker root. What it finds
// is always offered for confirmation: the destination comes from the config
// whatever is picked, so a wrong pick deploys the wrong image successfully.
func (m WorkflowModel) enterDockerfileScan() (tea.Model, tea.Cmd) {
	svcName := m.svcName
	buildRoot := m.dockerBuildRoot()

	searchRoot := buildRoot
	if prefix := wfProjectPrefix(svcName); prefix != "" {
		projectDir := filepath.Join(buildRoot, prefix)
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			searchRoot = projectDir
		}
	}
	m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  Scansione Dockerfile in %s...", searchRoot)))

	files, err := logic.FindDockerfiles(searchRoot)
	if err != nil || len(files) == 0 && searchRoot != buildRoot {
		files, _ = logic.FindDockerfiles(buildRoot)
	}

	if len(files) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun Dockerfile trovato in %s", buildRoot)))
		return m, tea.Quit
	}
	return m.enterDockerfileList(files)
}

func (m WorkflowModel) enterDockerfileList(files []string) (tea.Model, tea.Cmd) {
	m.state = wfSvcDockerfile
	items := dockerfileItems(files, m.dockerBuildRoot())
	m.list = listModel{
		title: wfDockerfileListTitle(m.svcName, m.svc.ECRRepository),
		items: items,
		width: m.width,
	}
	return m, m.list.Init()
}

// wfDockerfileListTitle names the destination in the title: it comes from the
// config whatever file is picked, so a mismatch is visible while choosing.
func wfDockerfileListTitle(serviceName, ecrRepository string) string {
	title := "Seleziona Dockerfile da buildare come " + serviceName
	if ecrRepository != "" {
		if idx := strings.Index(ecrRepository, "/"); idx != -1 {
			title += " → " + ecrRepository[idx+1:]
		}
	}
	return title
}

func (m WorkflowModel) finishDockerfile() (tea.Model, tea.Cmd) {
	m.dockerfilePath = m.list.selected
	m.discovered = true
	m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+m.dockerfilePath))
	return m.enterTagInput()
}

// shouldPersistDockerfilePath reports whether a discovered path is worth saving
// back to the config. A service that already declares one keeps it: overwriting
// would replace a correct value with one found on the wrong branch.
func (m WorkflowModel) shouldPersistDockerfilePath() bool {
	return m.discovered && m.svc.DockerfileSubpath == "" && !m.testUI
}

// ── Tag input ─────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterTagInput() (tea.Model, tea.Cmd) {
	m.state = wfSvcTagInput
	suggested, err := logic.IncrementPatch(m.svc.LastTag)
	if err != nil {
		suggested = m.svc.LastTag
	}
	m.suggestedTag = suggested
	m.input = newInputModel("Tag immagine", suggested, suggested)
	m.input.width = m.width
	return m, m.input.Init()
}

func (m WorkflowModel) finishTagInput() (tea.Model, tea.Cmd) {
	val := m.input.textInput.Value()
	if val == "" {
		val = m.suggestedTag
	}
	m.newTag = val

	if buildRoot := m.dockerBuildRoot(); m.shouldPersistDockerfilePath() && buildRoot != "" {
		rel, relErr := filepath.Rel(buildRoot, m.dockerfilePath)
		if relErr != nil {
			rel = m.dockerfilePath
		}
		if err := config.UpdateServiceDockerfilePath(m.svcName, rel); err != nil {
			m.log = append(m.log, WarnStyle.Render("  ⚠  impossibile salvare path Dockerfile: "+err.Error()))
		} else {
			m.log = append(m.log, SuccessStyle.Render("  ✓  Path Dockerfile salvato."))
		}
	}

	m.log = append(m.log, wfTagCard(m.svcName, m.oldTag, m.newTag))

	if dockerArgs, err := logic.ParseDockerfileArgs(m.dockerfilePath); err == nil && len(dockerArgs) > 0 {
		m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  %d build ARG rilevati nel Dockerfile", len(dockerArgs))))
		m.buildArgQueue = dockerArgs
		m.buildArgIdx = 0
		return m.enterBuildArg()
	}
	return m.enterBuild()
}

// ── Build args ────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterBuildArg() (tea.Model, tea.Cmd) {
	if m.buildArgIdx >= len(m.buildArgQueue) {
		return m.enterBuild()
	}
	m.state = wfSvcBuildArg
	arg := m.buildArgQueue[m.buildArgIdx]
	m.input = newInputModel(fmt.Sprintf("Build ARG: %s", arg.Name), arg.Default, arg.Default)
	m.input.width = m.width
	return m, m.input.Init()
}

func (m WorkflowModel) finishBuildArg() (tea.Model, tea.Cmd) {
	arg := m.buildArgQueue[m.buildArgIdx]
	val := m.input.textInput.Value()
	if val != "" {
		m.buildArgs[arg.Name] = val
	}
	m.buildArgIdx++
	return m.enterBuildArg()
}

// ── Build ─────────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterBuild() (tea.Model, tea.Cmd) {
	m.valuesPath = filepath.Join(m.helmValuesRoot(), m.svc.HelmValuesPath)
	m.chartVersion = m.svc.ChartVersion
	if m.chartVersion == "" {
		m.chartVersion = m.cfg.Config.ChartVersion
	}
	if m.svc.HelmImagePath != "" {
		m.helmSetArg = fmt.Sprintf("%s=%s:%s", m.svc.HelmSetKey, m.svc.HelmImagePath, m.newTag)
	} else {
		m.helmSetArg = fmt.Sprintf("%s=%s", m.svc.HelmSetKey, m.newTag)
	}

	if m.dryRun {
		buildArgStr := ""
		for k, v := range m.buildArgs {
			buildArgStr += fmt.Sprintf(" --build-arg %s=%s", k, v)
		}
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker build --no-cache -t %s:%s -f %s%s %s",
			m.svc.ECRRepository, m.newTag, m.dockerfilePath, buildArgStr, filepath.Dir(m.dockerfilePath))))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker push %s:%s", m.svc.ECRRepository, m.newTag)))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("helm upgrade %s %s -f %s --namespace %s --set %s --version %s",
			m.svc.ReleaseName, m.svc.ChartName, m.valuesPath, m.svc.Namespace, m.helmSetArg, m.chartVersion)))
		m.log = append(m.log, "\n"+SecondaryStyle.Render(fmt.Sprintf(
			"  ◆  DRY-RUN  —  %s  %s → %s  (non deployato)", m.svcName, m.oldTag, m.newTag)))
		m.results = append(m.results, DeployResult{Service: m.svcName, OldTag: m.oldTag, NewTag: m.newTag, Skipped: true})
		m.svcIdx++
		return m.startNextService()
	}

	m.state = wfSvcBuilding
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Docker Build --no-cache")
	svc := m.svc
	newTag := m.newTag
	dockerfilePath := m.dockerfilePath
	buildArgs := m.buildArgs
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(2500 * time.Millisecond)
			return wfOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.DockerBuild(svc.ECRRepository, newTag, dockerfilePath, buildArgs, &buf)
		return wfOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Push ──────────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterPush() (tea.Model, tea.Cmd) {
	m.state = wfSvcPushing
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Docker Push")
	svc := m.svc
	newTag := m.newTag
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1500 * time.Millisecond)
			return wfOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.DockerPush(svc.ECRRepository, newTag, &buf)
		return wfOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Helm ──────────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterHelm() (tea.Model, tea.Cmd) {
	m.state = wfSvcHelm
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Helm Deploy")
	svc := m.svc
	valuesPath := m.valuesPath
	helmSetArg := m.helmSetArg
	chartVersion := m.chartVersion
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1000 * time.Millisecond)
			return wfOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.HelmDeploy(svc.ReleaseName, svc.ChartName, valuesPath, svc.Namespace, helmSetArg, chartVersion, &buf)
		return wfOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Helm error recovery ───────────────────────────────────────────────────────

func (m WorkflowModel) enterHelmError() (tea.Model, tea.Cmd) {
	m.state = wfSvcHelmError
	m.list = listModel{
		title: "Cosa vuoi fare?",
		items: []Item{
			{Value: "retry", Label: "Riprova — esegui 'helm repo update' poi riprova"},
			{Value: "rollback", Label: "Rollback — rimuove l'immagine da ECR e annulla"},
			{Value: "cancel", Label: "Annulla  — esce senza deploy (immagine resta su ECR)"},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m WorkflowModel) finishHelmError() (tea.Model, tea.Cmd) {
	switch m.list.selected {
	case "retry":
		return m.enterHelm()
	case "rollback":
		return m.enterRollback()
	default:
		m.cancelled = true
		return m, tea.Quit
	}
}

// ── Rollback ──────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterRollback() (tea.Model, tea.Cmd) {
	m.state = wfSvcRollback
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("ECR Rollback")
	ecrRegion := m.cfg.Config.ECRRegion
	ecrRepo := m.svc.ECRRepository
	newTag := m.newTag
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		err := logic.ECRDeleteImage(ecrRegion, ecrRepo, newTag)
		return wfOpDoneMsg{err: err}
	})
}

// ── Post-deploy sync ──────────────────────────────────────────────────────────

// enterPostSync runs once, after every selected service has been deployed: one
// commit for the whole run means fewer pushes and fewer races.
func (m WorkflowModel) enterPostSync() (tea.Model, tea.Cmd) {
	if len(m.deployed) == 0 {
		return m.enterSummary()
	}
	if m.localSources {
		// The cluster runs a values file that exists in no commit: the tag on
		// the shared branch would describe an unreproducible state.
		m.log = append(m.log, WarnStyle.Render(
			"  ⚠  Sync saltato: deploy di prova dalle copie di lavoro"))
		return m.enterSummary()
	}
	if m.dryRun || m.testUI {
		if m.dryRun {
			m.log = append(m.log, wfDryRunLine("aggiornamento values + commit e push sul repo gestito"))
		}
		return m.enterSummary()
	}

	m.state = wfPostSync
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Sync repo")
	cfg := m.cfg
	deployed := m.deployed
	helmBranch := m.helmBranch
	dockerBranch := m.dockerBranch
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		lines, err := wfRunPostDeploySync(cfg, deployed, helmBranch, dockerBranch)
		return wfPostSyncDoneMsg{lines: lines, err: err}
	})
}

func (m WorkflowModel) finishService() (tea.Model, tea.Cmd) {
	m.results = append(m.results, DeployResult{
		Service: m.svcName,
		OldTag:  m.oldTag,
		NewTag:  m.newTag,
		Elapsed: time.Since(m.svcStart),
	})
	m.svcIdx++
	return m.startNextService()
}

// ── Summary ───────────────────────────────────────────────────────────────────

func (m WorkflowModel) enterSummary() (tea.Model, tea.Cmd) {
	m.state = wfSummary
	return m, tea.Quit
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m WorkflowModel) View() tea.View {
	var sb strings.Builder
	sb.WriteString(m.renderStepTracker())
	for _, line := range m.log {
		sb.WriteString(line + "\n")
	}
	switch m.state {
	case wfECRLogin, wfSvcBuilding, wfSvcPushing, wfSvcHelm, wfSvcRollback,
		wfBranchLoading, wfRepoPrep, wfDockerBranchLoading, wfDockerRepoPrep, wfPostSync:
		elapsed := DimStyle.Render(formatElapsed(time.Since(m.opStart)))
		sb.WriteString(fmt.Sprintf("  %s  %s\n", m.spinnerFrame(), elapsed))
	case wfServiceSelect:
		sb.WriteString(m.multisel.View().Content)
	case wfSvcTagSync:
		sb.WriteString(m.confirm.View().Content)
	case wfBranchSelect, wfDockerBranchSelect, wfSvcDockerfile, wfSvcDockerfileMissing, wfSvcHelmError:
		sb.WriteString(m.list.View().Content)
	case wfSvcTagInput, wfSvcBuildArg:
		sb.WriteString(m.input.View().Content)
	case wfSummary:
		w := m.width
		if w == 0 {
			w = 80
		}
		if bar := renderStatusBar(w); bar != "" {
			sb.WriteString("\n" + bar)
		}
	}
	return tea.NewView(sb.String())
}

// ── Step tracker (tab bar) ────────────────────────────────────────────────────

func (m WorkflowModel) renderStepTracker() string {
	var sb strings.Builder
	sb.WriteString("\n")

	// Tab ECR Login
	var ecrTab string
	if m.state == wfECRLogin {
		ecrTab = m.spinnerFrame() + " " + ValueStyle.Render("ECR")
	} else {
		ecrTab = SuccessStyle.Render("✓ ECR")
	}

	// Tab Selezione Servizi
	var svcTab string
	switch {
	case m.state < wfServiceSelect:
		svcTab = DimStyle.Render("· Servizi")
	case m.state == wfServiceSelect:
		svcTab = CursorStyle.Render("▸") + " " + ValueStyle.Render("Servizi")
	default:
		svcTab = SuccessStyle.Render("✓ Servizi")
	}

	// Tab Pipeline
	inPipeline := m.state >= wfSvcTagSync
	var pipeTab string
	if !inPipeline {
		pipeTab = DimStyle.Render("· Pipeline")
	} else {
		total := len(m.selectedServices)
		cur := m.svcIdx + 1
		if cur > total {
			cur = total
		}
		pipeTab = CursorStyle.Render("▸") + "  " +
			ValueStyle.Render(fmt.Sprintf("Pipeline [%d/%d]", cur, total)) +
			"  " + SelectedItemStyle.Render(strings.ToUpper(m.svcName))
	}

	div := DimStyle.Render("   │   ")
	sb.WriteString("  " + ecrTab + div + svcTab + div + pipeTab + "\n")

	// Separatore orizzontale
	w := m.width
	if w == 0 {
		w = 80
	}
	sb.WriteString(DimStyle.Render("  "+strings.Repeat("─", w-4)) + "\n")

	// Sub-stages (solo in pipeline)
	if inPipeline {
		sb.WriteString("    " + m.renderPipelineStages() + "\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

func (m WorkflowModel) spinnerFrame() string {
	return m.spinner.spinner.View()
}

func (m WorkflowModel) renderPipelineStages() string {
	type stStatus int
	const (
		stPending stStatus = iota
		stInteractive
		stSpinning
		stFailed
		stDone
	)

	asyncStatus := func(activeAt, doneAt wfState) stStatus {
		if m.state >= doneAt {
			return stDone
		}
		if m.state == activeAt {
			return stSpinning
		}
		return stPending
	}

	configStatus := func() stStatus {
		if m.state >= wfSvcBuilding {
			return stDone
		}
		switch m.state {
		case wfSvcTagSync, wfSvcDockerfile, wfSvcDockerfileMissing, wfSvcTagInput, wfSvcBuildArg:
			return stInteractive
		}
		return stPending
	}

	deployStatus := func() stStatus {
		if m.state >= wfPostSync {
			return stDone
		}
		if m.state == wfSvcHelm || m.state == wfSvcRollback {
			return stSpinning
		}
		if m.state == wfSvcHelmError {
			return stFailed
		}
		return stPending
	}

	type stageEntry struct {
		label  string
		status stStatus
	}
	stages := []stageEntry{
		{"Config", configStatus()},
		{"Build", asyncStatus(wfSvcBuilding, wfSvcPushing)},
		{"Push", asyncStatus(wfSvcPushing, wfSvcHelm)},
		{"Deploy", deployStatus()},
		{"Sync", asyncStatus(wfPostSync, wfSummary)},
	}

	frame := m.spinnerFrame()
	var parts []string
	for _, s := range stages {
		var indicator, lbl string
		switch s.status {
		case stDone:
			indicator = SuccessStyle.Render("✓")
			lbl = DimStyle.Render(s.label)
		case stSpinning:
			indicator = frame
			lbl = ValueStyle.Render(s.label)
		case stInteractive:
			indicator = CursorStyle.Render("▸")
			lbl = ValueStyle.Render(s.label)
		case stFailed:
			indicator = ErrStyle.Render("✗")
			lbl = ErrStyle.Render(s.label)
		default:
			indicator = DimStyle.Render("·")
			lbl = DimStyle.Render(s.label)
		}
		parts = append(parts, indicator+" "+lbl)
	}

	sep := DimStyle.Render("  →  ")
	var out strings.Builder
	for i, p := range parts {
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(p)
	}
	return out.String()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func wfDryRunLine(msg string) string {
	return SecondaryStyle.Render("  ◆ DRY-RUN") + "  " + DimStyle.Render(msg)
}

// wfTagCard is the log entry announcing the tag a service goes out with. The
// blank line on each side frames it as a break in the log rather than as a
// heading for the lines that follow.
func wfTagCard(serviceName, oldTag, newTag string) string {
	content := fmt.Sprintf("  %s    %s  →  %s  ",
		SelectedItemStyle.Render(serviceName),
		DimStyle.Render(oldTag),
		SuccessStyle.Render(newTag),
	)
	return "\n" + BoxStyle.Render(content) + "\n"
}

func wfProjectPrefix(serviceName string) string {
	if idx := strings.Index(serviceName, "-"); idx != -1 {
		return serviceName[:idx]
	}
	return ""
}

func wfSortedServiceKeys(m map[string]config.ServiceConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// wfRunPostDeploySync writes the deployed tags back to the repositories, one
// commit per repository. A failure is reported, never swallowed: the branch
// everybody deploys from would no longer match the cluster.
func wfRunPostDeploySync(cfg *config.Config, deployed []deployedService, helmBranch, dockerBranch string) ([]string, error) {
	var lines []string
	var failures []error

	message := logic.DeployCommitMessageFor(wfDeployedServices(deployed))
	reposRoot := config.GetReposRoot()

	if targets := wfHelmTargets(deployed); len(targets) > 0 && cfg.Config.HelmRootPath != "" {
		ls, err := wfSyncRepo(
			cfg.Config.HelmRootPath, helmBranch, reposRoot, message, "chart",
			func(dir string) ([]string, error) {
				var changed []string
				for _, d := range targets {
					path := filepath.Join(dir, d.svc.HelmValuesPath)
					if err := logic.UpdateHelmValuesTag(path, d.svc.HelmSetKey, d.tag, d.svc.HelmImagePath); err != nil {
						return nil, fmt.Errorf("%s: %w", d.name, err)
					}
					changed = append(changed, d.svc.HelmValuesPath)
				}
				return wfUnique(changed), nil
			})
		lines = append(lines, ls...)
		if err != nil {
			failures = append(failures, err)
		}
	}

	if targets := wfManifestTargets(deployed); len(targets) > 0 && cfg.Config.DockerRootPath != "" {
		ls, err := wfSyncRepo(
			cfg.Config.DockerRootPath, dockerBranch, reposRoot, message, "manifest k8s",
			func(dir string) ([]string, error) {
				var changed []string
				for _, d := range targets {
					path := filepath.Join(dir, d.svc.K8sManifestPath)
					if err := logic.UpdateK8sManifestImage(path, d.svc.K8sImageRef, d.tag); err != nil {
						return nil, fmt.Errorf("%s: %w", d.name, err)
					}
					changed = append(changed, d.svc.K8sManifestPath)
				}
				return wfUnique(changed), nil
			})
		lines = append(lines, ls...)
		if err != nil {
			failures = append(failures, err)
		}
	}

	return lines, errors.Join(failures...)
}

// wfSyncRepo pushes one repository's share of the deploy and logs the outcome.
func wfSyncRepo(workingCopy, branch, reposRoot, message, label string,
	apply func(dir string) ([]string, error)) ([]string, error) {

	remoteURL, err := logic.GitRemoteURL(workingCopy)
	if err != nil {
		return []string{ErrStyle.Render("  ✗  Sync " + label + " non riuscito: " + err.Error())}, err
	}

	if err := logic.SyncToBranch(remoteURL, branch, reposRoot, message, apply, nil); err != nil {
		return []string{
			ErrStyle.Render("  ✗  Sync " + label + " non riuscito: " + err.Error()),
			WarnStyle.Render("      Il branch " + branch + " non riflette più lo stato del cluster."),
		}, err
	}

	return []string{SuccessStyle.Render("  ✓  Sync " + label + ": commit e push su " + branch)}, nil
}

func wfDeployedServices(deployed []deployedService) []logic.DeployedService {
	out := make([]logic.DeployedService, 0, len(deployed))
	for _, d := range deployed {
		out = append(out, logic.DeployedService{Name: d.name, Tag: d.tag})
	}
	return out
}

func wfHelmTargets(deployed []deployedService) []deployedService {
	var out []deployedService
	for _, d := range deployed {
		if d.svc.HelmValuesPath != "" && d.svc.HelmSetKey != "" {
			out = append(out, d)
		}
	}
	return out
}

func wfManifestTargets(deployed []deployedService) []deployedService {
	var out []deployedService
	for _, d := range deployed {
		if d.svc.K8sManifestPath != "" && d.svc.K8sImageRef != "" {
			out = append(out, d)
		}
	}
	return out
}

// wfUnique drops repeats: several services can share one values file.
func wfUnique(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
