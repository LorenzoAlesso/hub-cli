package ui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	tea "charm.land/bubbletea/v2"
)

// ── States ────────────────────────────────────────────────────────────────────

type psnState int

const (
	psnNsLoading psnState = iota
	psnNsSelect
	psnBranchConfirm
	psnBranchSwitch
	psnDepLoading
	psnDepSelect
	psnTagInput
	psnDockerfileList
	psnBuildArg
	psnBuilding
	psnPushing
	psnSettingImage
	psnRollingOut
	psnDeployError
	psnRollback
	psnSummary
)

// ── Messages ──────────────────────────────────────────────────────────────────

type psnOpDoneMsg struct {
	err    error
	output []byte
}

type psnNsLoadedMsg struct {
	namespaces []string
	err        error
}

type psnDepLoadedMsg struct {
	deployments []logic.DeploymentInfo
	err         error
}

// ── Model ─────────────────────────────────────────────────────────────────────

// PSNWorkflowModel drives the PSN deploy pipeline. Unlike the local workflow,
// services are discovered live from the cluster (namespace → deployments) and
// the deploy is kubectl-based (set image + rollout), with no Helm involved.
type PSNWorkflowModel struct {
	cfg     *config.Config
	cluster config.PSNClusterConfig
	dryRun  bool
	testUI  bool

	state     psnState
	width     int
	cancelled bool

	log []string // rendered history lines

	spinner  spinnerModel
	multisel multiSelectModel
	input    inputModel
	list     listModel
	confirm  confirmModel

	opStart time.Time

	namespace      string
	project        *config.PSNProjectConfig // per-namespace Dockerfile resolution override
	originalBranch string                   // project repo branch before the guard's checkout
	branchSwitched bool                     // restore originalBranch once the workflow ends
	depByName      map[string]logic.DeploymentInfo
	selectedDeps   []string
	depIdx         int

	dep            logic.DeploymentInfo
	repo           string // registry + path, no tag
	oldTag         string
	newTag         string
	suggestedTag   string
	dockerfilePath string
	buildArgs      map[string]string
	buildArgQueue  []logic.DockerArg
	buildArgIdx    int
	imageApplied   bool // set image succeeded: rollback must also undo the rollout
	depStart       time.Time

	results []DeployResult
}

// RunPSNWorkflow runs the PSN deploy pipeline as a single persistent BubbleTea
// program. Returns (results, cancelled, error).
func RunPSNWorkflow(cfg *config.Config, cluster config.PSNClusterConfig, dryRun, testUI bool) ([]DeployResult, bool, error) {
	label := "PSN — " + cluster.Name
	if testUI {
		label = "TEST-UI PSN"
	}
	SetStatus(label, cluster.AKSName)

	m := PSNWorkflowModel{
		cfg:     cfg,
		cluster: cluster,
		dryRun:  dryRun,
		testUI:  testUI,
		state:   psnNsLoading,
		spinner: newSpinnerModel("Lettura namespace"),
		opStart: time.Now(),
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	ClearStatus()
	if err != nil {
		return nil, false, err
	}
	wf := final.(PSNWorkflowModel)
	wf.restoreProjectBranch()
	return wf.results, wf.cancelled, nil
}

// restoreProjectBranch puts the project repo back on its original branch after
// the workflow's checkout: the repo is shared with the local workflow, which
// must not inherit a PSN branch. Runs after the TUI has ended.
func (m PSNWorkflowModel) restoreProjectBranch() {
	if !m.branchSwitched || m.originalBranch == "" || m.project == nil {
		return
	}
	clean, err := logic.GitIsClean(m.project.DockerRoot)
	if err != nil || !clean {
		PrintWarn(fmt.Sprintf("Branch %q non ripristinato su %s: repo non pulito", m.originalBranch, m.project.DockerRoot))
		return
	}
	if err := logic.GitCheckout(m.project.DockerRoot, m.originalBranch); err != nil {
		PrintWarn("Ripristino branch fallito: " + err.Error())
		return
	}
	PrintOK(fmt.Sprintf("Repo Docker ripristinato sul branch %q", m.originalBranch))
}

// In dry-run/test-ui the kube context is not pointed at the PSN cluster, so
// discovery runs on placeholder data.
func psnFakeNamespaces() []string {
	return []string{"demo-ns-col", "demo-ns2-col"}
}

func psnFakeDeployments() []logic.DeploymentInfo {
	return []logic.DeploymentInfo{
		{Name: "webapp", Container: "webapp", Image: "demoacr.azurecr.io/demo/webapp:1.0.0", Containers: 1},
		{Name: "jboss-fe", Container: "jboss-fe", Image: "demoacr.azurecr.io/demo/jboss-fe:2.1.3", Containers: 1},
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) Init() tea.Cmd {
	dryRun := m.dryRun
	testUI := m.testUI
	return tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(600 * time.Millisecond)
			return psnNsLoadedMsg{namespaces: psnFakeNamespaces()}
		}
		if dryRun {
			return psnNsLoadedMsg{namespaces: psnFakeNamespaces()}
		}
		ns, err := logic.ListAppNamespaces()
		return psnNsLoadedMsg{namespaces: ns, err: err}
	})
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}
	if wm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wm.Width
		return m, nil
	}
	if ns, ok := msg.(psnNsLoadedMsg); ok {
		return m.handleNsLoaded(ns)
	}
	if dep, ok := msg.(psnDepLoadedMsg); ok {
		return m.handleDepLoaded(dep)
	}
	if done, ok := msg.(psnOpDoneMsg); ok {
		return m.handleOpDone(done)
	}
	return m.forwardToActive(msg)
}

func (m PSNWorkflowModel) forwardToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case psnNsLoading, psnBranchSwitch, psnDepLoading, psnBuilding, psnPushing, psnSettingImage, psnRollingOut, psnRollback:
		sm, cmd := m.spinner.Update(msg)
		m.spinner = sm.(spinnerModel)
		return m, cmd

	case psnBranchConfirm:
		sm, cmd := m.confirm.Update(msg)
		m.confirm = sm.(confirmModel)
		if m.confirm.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.confirm.done {
			return m.finishBranchConfirm()
		}
		return m, cmd

	case psnNsSelect, psnDockerfileList, psnDeployError:
		sm, cmd := m.list.Update(msg)
		m.list = sm.(listModel)
		if m.list.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.list.done {
			switch m.state {
			case psnNsSelect:
				return m.finishNsSelect()
			case psnDockerfileList:
				return m.finishDockerfile()
			default:
				return m.finishDeployError()
			}
		}
		return m, cmd

	case psnDepSelect:
		sm, cmd := m.multisel.Update(msg)
		m.multisel = sm.(multiSelectModel)
		if m.multisel.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.multisel.done {
			return m.finishDepSelect()
		}
		return m, cmd

	case psnTagInput, psnBuildArg:
		sm, cmd := m.input.Update(msg)
		m.input = sm.(inputModel)
		if m.input.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.input.done {
			if m.state == psnTagInput {
				return m.finishTagInput()
			}
			return m.finishBuildArg()
		}
		return m, cmd
	}
	return m, nil
}

// ── Namespace selection ───────────────────────────────────────────────────────

func (m PSNWorkflowModel) handleNsLoaded(msg psnNsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  Lettura namespace fallita: "+msg.err.Error()))
		return m, tea.Quit
	}
	if len(msg.namespaces) == 0 {
		m.log = append(m.log, ErrStyle.Render("  ✗  Nessun namespace applicativo trovato sul cluster"))
		return m, tea.Quit
	}
	if m.dryRun {
		m.log = append(m.log, SecondaryStyle.Render("  ◆ DRY-RUN")+"  "+
			DimStyle.Render("contesto cluster non agganciato: namespace e deployment sono simulati"))
	}

	m.state = psnNsSelect
	items := make([]Item, len(msg.namespaces))
	for i, ns := range msg.namespaces {
		items[i] = Item{Value: ns, Label: ns}
	}
	m.list = listModel{title: "Namespace", items: items, width: m.width}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishNsSelect() (tea.Model, tea.Cmd) {
	m.namespace = m.list.selected

	if !m.testUI {
		m.project = m.cfg.PSN.ProjectForNamespace(m.namespace)
	}
	if m.project != nil {
		m.log = append(m.log, DimStyle.Render("  ·  Progetto Docker dedicato: "+m.project.DockerRoot))
		if want := m.project.ExpectedBranch(m.cluster); want != "" {
			return m.checkProjectBranch(want)
		}
	}
	return m.enterDepLoading()
}

// ── Project branch guard ──────────────────────────────────────────────────────

// checkProjectBranch verifies the project repo is on the branch expected for
// the cluster environment: the branch determines what gets baked into the
// images (certs, source branches), so building from the wrong one is blocked.
func (m PSNWorkflowModel) checkProjectBranch(want string) (tea.Model, tea.Cmd) {
	current, err := logic.GitCurrentBranch(m.project.DockerRoot)
	if err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  "+err.Error()))
		return m, tea.Quit
	}
	if current == want {
		m.log = append(m.log, SuccessStyle.Render(fmt.Sprintf("  ✓  Branch Docker %q corretto per l'ambiente", current)))
		return m.enterDepLoading()
	}
	if m.dryRun {
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("git checkout %s  (repo %s, branch attuale %q)",
			want, m.project.DockerRoot, current)))
		return m.enterDepLoading()
	}

	m.originalBranch = current
	m.state = psnBranchConfirm
	m.confirm = confirmModel{
		title: "Branch del progetto Docker non corretto — checkout?",
		body: fmt.Sprintf("  %s  %s\n  %s  %s\n  %s  %s",
			LabelStyle.Render("Repo:    "), ValueStyle.Render(m.project.DockerRoot),
			LabelStyle.Render("Attuale: "), WarnStyle.Render(current),
			LabelStyle.Render("Atteso:  "), SuccessStyle.Render(want)),
		width: m.width,
	}
	return m, m.confirm.Init()
}

func (m PSNWorkflowModel) finishBranchConfirm() (tea.Model, tea.Cmd) {
	want := m.project.ExpectedBranch(m.cluster)
	if m.confirm.choice != 0 {
		// Building from the wrong branch would bake the wrong environment
		// into the images: refusing the checkout stops the deploy.
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Deploy annullato: il repo non è sul branch %q", want)))
		m.cancelled = true
		return m, tea.Quit
	}

	clean, err := logic.GitIsClean(m.project.DockerRoot)
	if err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  "+err.Error()))
		return m, tea.Quit
	}
	if !clean {
		m.log = append(m.log, ErrStyle.Render("  ✗  Il repo ha modifiche non committate: checkout non sicuro. Sistemalo e rilancia."))
		m.cancelled = true
		return m, tea.Quit
	}

	m.state = psnBranchSwitch
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("git checkout " + want)
	root := m.project.DockerRoot
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		return psnOpDoneMsg{err: logic.GitCheckout(root, want)}
	})
}

// ── Deployment loading ────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterDepLoading() (tea.Model, tea.Cmd) {
	m.state = psnDepLoading
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Lettura deployment in " + m.namespace)

	namespace := m.namespace
	simulated := m.testUI || m.dryRun
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if simulated {
			if testUI {
				time.Sleep(600 * time.Millisecond)
			}
			return psnDepLoadedMsg{deployments: psnFakeDeployments()}
		}
		deployments, err := logic.ListDeployments(namespace)
		return psnDepLoadedMsg{deployments: deployments, err: err}
	})
}

// ── Deployment selection ──────────────────────────────────────────────────────

func (m PSNWorkflowModel) handleDepLoaded(msg psnDepLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  Lettura deployment fallita: "+msg.err.Error()))
		return m, tea.Quit
	}
	if len(msg.deployments) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun deployment trovato in %q", m.namespace)))
		return m, tea.Quit
	}

	m.depByName = make(map[string]logic.DeploymentInfo, len(msg.deployments))
	items := make([]Item, len(msg.deployments))
	for i, dep := range msg.deployments {
		m.depByName[dep.Name] = dep
		_, tag := logic.SplitImageRef(dep.Image)
		if tag == "" {
			tag = "(senza tag)"
		}
		items[i] = Item{Value: dep.Name, Label: dep.Name, Desc: tag}
	}

	m.state = psnDepSelect
	m.multisel = multiSelectModel{
		title:    "Servizi da deployare",
		items:    items,
		selected: make(map[int]bool),
		width:    m.width,
	}
	return m, m.multisel.Init()
}

func (m PSNWorkflowModel) finishDepSelect() (tea.Model, tea.Cmd) {
	selected := make([]string, 0, len(m.multisel.selected))
	for i, item := range m.multisel.items {
		if m.multisel.selected[i] {
			selected = append(selected, item.Value)
		}
	}
	m.selectedDeps = selected
	m.depIdx = 0
	return m.startNextDeployment()
}

// ── Per-deployment pipeline ───────────────────────────────────────────────────

func (m PSNWorkflowModel) startNextDeployment() (tea.Model, tea.Cmd) {
	if m.depIdx >= len(m.selectedDeps) {
		m.state = psnSummary
		return m, tea.Quit
	}

	name := m.selectedDeps[m.depIdx]
	m.dep = m.depByName[name]
	m.repo, m.oldTag = logic.SplitImageRef(m.dep.Image)
	m.newTag = ""
	m.dockerfilePath = ""
	m.buildArgs = make(map[string]string)
	m.buildArgQueue = nil
	m.buildArgIdx = 0
	m.imageApplied = false
	m.depStart = time.Now()

	SetStatus(name, m.cluster.Name)

	if len(m.selectedDeps) > 1 {
		m.log = append(m.log, "\n"+SectionStyle.Render(fmt.Sprintf(
			"── Servizio [%d/%d]: %s", m.depIdx+1, len(m.selectedDeps), strings.ToUpper(name))))
	}
	m.log = append(m.log, DimStyle.Render("  ·  Immagine corrente: "+m.dep.Image))
	if m.dep.Containers > 1 {
		m.log = append(m.log, WarnStyle.Render(fmt.Sprintf(
			"  ⚠  %d container nel pod template — verrà aggiornato %q", m.dep.Containers, m.dep.Container)))
	}

	return m.enterTagInput()
}

// ── Tag input ─────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterTagInput() (tea.Model, tea.Cmd) {
	m.state = psnTagInput
	suggested, err := logic.IncrementPatch(m.oldTag)
	if err != nil {
		suggested = m.oldTag
	}
	m.suggestedTag = suggested
	m.input = newInputModel("Tag immagine", suggested, suggested)
	m.input.width = m.width
	return m, m.input.Init()
}

func (m PSNWorkflowModel) finishTagInput() (tea.Model, tea.Cmd) {
	val := m.input.textInput.Value()
	if val == "" {
		val = m.suggestedTag
	}
	m.newTag = val

	content := fmt.Sprintf("  %s    %s  →  %s  ",
		SelectedItemStyle.Render(m.dep.Name),
		DimStyle.Render(m.oldTag),
		SuccessStyle.Render(m.newTag),
	)
	m.log = append(m.log, "\n"+BoxStyle.Render(content))

	return m.resolveDockerfile()
}

// ── Dockerfile resolve ────────────────────────────────────────────────────────

// resolveDockerfile finds the Dockerfile for the current deployment. With a
// per-namespace project configured, resolution happens inside its docker_root;
// otherwise via the psn.deployments → local service mapping, then by scanning
// docker_root_path.
func (m PSNWorkflowModel) resolveDockerfile() (tea.Model, tea.Cmd) {
	if m.testUI {
		m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: (simulato)"))
		return m.enterBuild()
	}

	if m.project != nil {
		return m.resolveProjectDockerfile()
	}

	if svcName, ok := m.cfg.PSN.Deployments[strings.ToLower(m.dep.Name)]; ok && svcName != "" {
		if svc, found := m.cfg.Services[svcName]; found && svc.DockerfileSubpath != "" {
			full := filepath.Join(m.cfg.Config.DockerRootPath, svc.DockerfileSubpath)
			if abs, err := filepath.Abs(full); err == nil {
				if _, err := os.Stat(abs); err == nil {
					m.dockerfilePath = abs
					m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  Dockerfile (da %s): %s", svcName, abs)))
					return m.afterDockerfile()
				}
				m.log = append(m.log, WarnStyle.Render("  ⚠  Dockerfile del servizio mappato non trovato: "+abs))
			}
		} else {
			m.log = append(m.log, WarnStyle.Render(fmt.Sprintf(
				"  ⚠  Mapping %q → servizio %q non risolvibile in config", m.dep.Name, svcName)))
		}
	}

	searchRoot := m.cfg.Config.DockerRootPath
	m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  Scansione Dockerfile in %s...", searchRoot)))

	files, err := logic.FindDockerfiles(searchRoot)
	if err != nil || len(files) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun Dockerfile trovato in %s", searchRoot)))
		return m, tea.Quit
	}
	if len(files) == 1 {
		m.dockerfilePath = files[0]
		m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+files[0]))
		return m.afterDockerfile()
	}

	m.state = psnDockerfileList
	items := make([]Item, len(files))
	for i, f := range files {
		items[i] = Item{Value: f, Label: f}
	}
	m.list = listModel{title: "Seleziona Dockerfile", items: items, width: m.width}
	return m, m.list.Init()
}

// resolveProjectDockerfile resolves inside the project's docker_root:
// explicit mapping → convention "<deployment>/Dockerfile" → full scan.
func (m PSNWorkflowModel) resolveProjectDockerfile() (tea.Model, tea.Cmd) {
	root := m.project.DockerRoot

	if rel, ok := m.project.Deployments[strings.ToLower(m.dep.Name)]; ok && rel != "" {
		full := filepath.Join(root, rel)
		if _, err := os.Stat(full); err == nil {
			m.dockerfilePath = full
			m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+full))
			return m.afterDockerfile()
		}
		m.log = append(m.log, WarnStyle.Render("  ⚠  Dockerfile mappato non trovato: "+full))
	}

	conventional := filepath.Join(root, m.dep.Name, "Dockerfile")
	if _, err := os.Stat(conventional); err == nil {
		m.dockerfilePath = conventional
		m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+conventional))
		return m.afterDockerfile()
	}

	m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  Scansione Dockerfile in %s...", root)))
	files, err := logic.FindDockerfiles(root)
	if err != nil || len(files) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun Dockerfile trovato in %s", root)))
		return m, tea.Quit
	}
	if len(files) == 1 {
		m.dockerfilePath = files[0]
		m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+files[0]))
		return m.afterDockerfile()
	}

	m.state = psnDockerfileList
	items := make([]Item, len(files))
	for i, f := range files {
		items[i] = Item{Value: f, Label: f}
	}
	m.list = listModel{title: "Seleziona Dockerfile", items: items, width: m.width}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishDockerfile() (tea.Model, tea.Cmd) {
	m.dockerfilePath = m.list.selected
	m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+m.dockerfilePath))
	return m.afterDockerfile()
}

func (m PSNWorkflowModel) afterDockerfile() (tea.Model, tea.Cmd) {
	if dockerArgs, err := logic.ParseDockerfileArgs(m.dockerfilePath); err == nil && len(dockerArgs) > 0 {
		m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  %d build ARG rilevati nel Dockerfile", len(dockerArgs))))
		m.buildArgQueue = dockerArgs
		m.buildArgIdx = 0
		return m.enterBuildArg()
	}
	return m.enterBuild()
}

// ── Build args ────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterBuildArg() (tea.Model, tea.Cmd) {
	if m.buildArgIdx >= len(m.buildArgQueue) {
		return m.enterBuild()
	}
	m.state = psnBuildArg
	arg := m.buildArgQueue[m.buildArgIdx]
	m.input = newInputModel(fmt.Sprintf("Build ARG: %s", arg.Name), arg.Default, arg.Default)
	m.input.width = m.width
	return m, m.input.Init()
}

func (m PSNWorkflowModel) finishBuildArg() (tea.Model, tea.Cmd) {
	arg := m.buildArgQueue[m.buildArgIdx]
	val := m.input.textInput.Value()
	if val != "" {
		m.buildArgs[arg.Name] = val
	}
	m.buildArgIdx++
	return m.enterBuildArg()
}

// ── Build ─────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterBuild() (tea.Model, tea.Cmd) {
	if m.dryRun {
		buildArgStr := ""
		for k, v := range m.buildArgs {
			buildArgStr += fmt.Sprintf(" --build-arg %s=%s", k, v)
		}
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker build --no-cache -t %s:%s -f %s%s %s",
			m.repo, m.newTag, m.dockerfilePath, buildArgStr, filepath.Dir(m.dockerfilePath))))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker push %s:%s", m.repo, m.newTag)))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("kubectl set image deployment/%s %s=%s:%s -n %s",
			m.dep.Name, m.dep.Container, m.repo, m.newTag, m.namespace)))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("kubectl rollout status deployment/%s -n %s --timeout 300s",
			m.dep.Name, m.namespace)))
		m.log = append(m.log, "\n"+SecondaryStyle.Render(fmt.Sprintf(
			"  ◆  DRY-RUN  —  %s  %s → %s  (non deployato)", m.dep.Name, m.oldTag, m.newTag)))
		m.results = append(m.results, DeployResult{Service: m.dep.Name, OldTag: m.oldTag, NewTag: m.newTag, Skipped: true})
		m.depIdx++
		return m.startNextDeployment()
	}

	m.state = psnBuilding
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Docker Build --no-cache")
	repo := m.repo
	newTag := m.newTag
	dockerfilePath := m.dockerfilePath
	buildArgs := m.buildArgs
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(2500 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.DockerBuild(repo, newTag, dockerfilePath, buildArgs, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Push ──────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterPush() (tea.Model, tea.Cmd) {
	m.state = psnPushing
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Docker Push (ACR)")
	repo := m.repo
	newTag := m.newTag
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1500 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.DockerPush(repo, newTag, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Deploy: set image + rollout ───────────────────────────────────────────────

func (m PSNWorkflowModel) enterSetImage() (tea.Model, tea.Cmd) {
	m.state = psnSettingImage
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("kubectl set image")
	namespace := m.namespace
	depName := m.dep.Name
	container := m.dep.Container
	image := m.repo + ":" + m.newTag
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(500 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.KubectlSetImage(namespace, depName, container, image, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

func (m PSNWorkflowModel) enterRollout() (tea.Model, tea.Cmd) {
	m.state = psnRollingOut
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Rollout in corso")
	namespace := m.namespace
	depName := m.dep.Name
	testUI := m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1200 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.KubectlRolloutStatus(namespace, depName, 5*time.Minute, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Deploy error recovery ─────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterDeployError() (tea.Model, tea.Cmd) {
	m.state = psnDeployError
	m.list = listModel{
		title: "Cosa vuoi fare?",
		items: []Item{
			{Value: "retry", Label: "Riprova  — riesegui set image e attendi il rollout"},
			{Value: "rollback", Label: "Rollback — ripristina il deployment e rimuove l'immagine da ACR"},
			{Value: "cancel", Label: "Annulla  — esce senza rimuovere l'immagine da ACR"},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishDeployError() (tea.Model, tea.Cmd) {
	switch m.list.selected {
	case "retry":
		return m.enterSetImage()
	case "rollback":
		return m.enterRollbackOp()
	default:
		m.cancelled = true
		return m, tea.Quit
	}
}

// ── Rollback ──────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterRollbackOp() (tea.Model, tea.Cmd) {
	m.state = psnRollback
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Rollback (deployment + ACR)")
	namespace := m.namespace
	depName := m.dep.Name
	acrName := m.cluster.ACRName
	repo := m.repo
	newTag := m.newTag
	imageApplied := m.imageApplied
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		var buf bytes.Buffer
		if imageApplied {
			if err := logic.KubectlRolloutUndo(namespace, depName, &buf); err != nil {
				return psnOpDoneMsg{err: err, output: buf.Bytes()}
			}
		}
		err := logic.ACRDeleteImage(acrName, repo, newTag)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── handleOpDone ──────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) handleOpDone(msg psnOpDoneMsg) (tea.Model, tea.Cmd) {
	elapsed := formatElapsed(time.Since(m.opStart))

	if msg.err != nil {
		switch m.state {
		case psnBranchSwitch:
			m.log = append(m.log, ErrStyle.Render("  ✗  "+msg.err.Error()))
			m.cancelled = true
			return m, tea.Quit
		case psnBuilding:
			m.log = append(m.log, ErrStyle.Render("  ✗  Docker Build fallito"))
			if len(msg.output) > 0 {
				m.log = append(m.log, DimStyle.Render(string(msg.output)))
			}
			return m, tea.Quit
		case psnPushing:
			m.log = append(m.log, ErrStyle.Render("  ✗  Docker Push fallito"))
			if len(msg.output) > 0 {
				m.log = append(m.log, DimStyle.Render(string(msg.output)))
			}
			return m, tea.Quit
		case psnSettingImage:
			m.log = append(m.log, ErrStyle.Render("  ✗  Set image fallito: "+msg.err.Error()))
			return m.enterDeployError()
		case psnRollingOut:
			m.log = append(m.log, ErrStyle.Render("  ✗  Rollout fallito: "+msg.err.Error()))
			if len(msg.output) > 0 {
				m.log = append(m.log, DimStyle.Render(string(msg.output)))
			}
			return m.enterDeployError()
		case psnRollback:
			m.log = append(m.log, ErrStyle.Render("  ✗  Rollback fallito: "+msg.err.Error()))
			m.cancelled = true
			return m, tea.Quit
		}
	}

	switch m.state {
	case psnBranchSwitch:
		m.branchSwitched = true
		m.log = append(m.log, SuccessStyle.Render("  ✓")+
			DimStyle.Render("  Checkout su "+m.project.ExpectedBranch(m.cluster)+"  ")+ValueStyle.Render(elapsed))
		return m.enterDepLoading()

	case psnBuilding:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Build  ")+ValueStyle.Render(elapsed))
		return m.enterPush()

	case psnPushing:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Push  ")+ValueStyle.Render(elapsed))
		return m.enterSetImage()

	case psnSettingImage:
		m.imageApplied = true
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Set image  ")+ValueStyle.Render(elapsed))
		return m.enterRollout()

	case psnRollingOut:
		m.log = append(m.log, SuccessStyle.Render("  ✓")+DimStyle.Render("  Rollout  ")+ValueStyle.Render(elapsed))
		m.results = append(m.results, DeployResult{
			Service: m.dep.Name,
			OldTag:  m.oldTag,
			NewTag:  m.newTag,
			Elapsed: time.Since(m.depStart),
		})
		m.depIdx++
		return m.startNextDeployment()

	case psnRollback:
		m.log = append(m.log, WarnStyle.Render("  ⚠  Rollback completato"))
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) View() tea.View {
	var sb strings.Builder
	sb.WriteString(m.renderTracker())
	for _, line := range m.log {
		sb.WriteString(line + "\n")
	}
	switch m.state {
	case psnNsLoading, psnBranchSwitch, psnDepLoading, psnBuilding, psnPushing, psnSettingImage, psnRollingOut, psnRollback:
		elapsed := DimStyle.Render(formatElapsed(time.Since(m.opStart)))
		sb.WriteString(fmt.Sprintf("  %s  %s\n", m.spinner.spinner.View(), elapsed))
	case psnBranchConfirm:
		sb.WriteString(m.confirm.View().Content)
	case psnNsSelect, psnDockerfileList, psnDeployError:
		sb.WriteString(m.list.View().Content)
	case psnDepSelect:
		sb.WriteString(m.multisel.View().Content)
	case psnTagInput, psnBuildArg:
		sb.WriteString(m.input.View().Content)
	case psnSummary:
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

func (m PSNWorkflowModel) renderTracker() string {
	var sb strings.Builder
	sb.WriteString("\n")

	// The Azure phase always completes before the TUI starts.
	azTab := SuccessStyle.Render("✓ Azure")

	var nsTab string
	switch {
	case m.state <= psnNsSelect:
		marker := CursorStyle.Render("▸")
		if m.state == psnNsLoading {
			marker = m.spinner.spinner.View()
		}
		nsTab = marker + " " + ValueStyle.Render("Namespace")
	default:
		nsTab = SuccessStyle.Render("✓ Namespace")
	}

	var depTab string
	switch {
	case m.state < psnDepLoading:
		depTab = DimStyle.Render("· Servizi")
	case m.state <= psnDepSelect:
		marker := CursorStyle.Render("▸")
		if m.state == psnDepLoading {
			marker = m.spinner.spinner.View()
		}
		depTab = marker + " " + ValueStyle.Render("Servizi")
	default:
		depTab = SuccessStyle.Render("✓ Servizi")
	}

	inPipeline := m.state >= psnTagInput
	var pipeTab string
	if !inPipeline {
		pipeTab = DimStyle.Render("· Pipeline")
	} else {
		total := len(m.selectedDeps)
		cur := m.depIdx + 1
		if cur > total {
			cur = total
		}
		pipeTab = CursorStyle.Render("▸") + "  " +
			ValueStyle.Render(fmt.Sprintf("Pipeline [%d/%d]", cur, total)) +
			"  " + SelectedItemStyle.Render(strings.ToUpper(m.dep.Name))
	}

	div := DimStyle.Render("   │   ")
	sb.WriteString("  " + azTab + div + nsTab + div + depTab + div + pipeTab + "\n")

	w := m.width
	if w == 0 {
		w = 80
	}
	sb.WriteString(DimStyle.Render("  "+strings.Repeat("─", w-4)) + "\n")

	if inPipeline {
		sb.WriteString("    " + m.renderPipelineStages() + "\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

func (m PSNWorkflowModel) renderPipelineStages() string {
	type stStatus int
	const (
		stPending stStatus = iota
		stInteractive
		stSpinning
		stFailed
		stDone
	)

	configStatus := func() stStatus {
		if m.state >= psnBuilding {
			return stDone
		}
		switch m.state {
		case psnTagInput, psnDockerfileList, psnBuildArg:
			return stInteractive
		}
		return stPending
	}

	asyncStatus := func(activeAt, doneAt psnState) stStatus {
		if m.state >= doneAt {
			return stDone
		}
		if m.state == activeAt {
			return stSpinning
		}
		return stPending
	}

	deployStatus := func() stStatus {
		if m.state >= psnSummary {
			return stDone
		}
		switch m.state {
		case psnSettingImage, psnRollingOut, psnRollback:
			return stSpinning
		case psnDeployError:
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
		{"Build", asyncStatus(psnBuilding, psnPushing)},
		{"Push", asyncStatus(psnPushing, psnSettingImage)},
		{"Deploy", deployStatus()},
	}

	frame := m.spinner.spinner.View()
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
