package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	tea "charm.land/bubbletea/v2"
)

// ── States ────────────────────────────────────────────────────────────────────

type psnState int

const (
	psnChartsPrep psnState = iota
	psnRepoPrep
	psnBranchSelect
	psnDepSelect
	psnTagInput
	psnDockerfileList
	psnDockerfileMissing
	psnBuildArg
	psnBuilding
	psnPushing
	psnHelmDeploy
	psnDeployError
	psnRestartConfirm
	psnRestarting
	psnSync
	psnSummary
)

// ── Messages ──────────────────────────────────────────────────────────────────

type psnOpDoneMsg struct {
	err    error
	output []byte
}

type psnRepoPrepDoneMsg struct {
	dir string
	err error
}

type psnBranchesLoadedMsg struct {
	branches []string
	err      error
}

// psnStartMsg kicks the workflow off from Update, which can mutate the model —
// Init cannot.
type psnStartMsg struct{}

type psnChartsPrepDoneMsg struct {
	dir string
	err error
}

type psnSyncDoneMsg struct {
	err error
}

// ── Model ─────────────────────────────────────────────────────────────────────

// PSNWorkflowModel drives the PSN deploy pipeline. PSN is deployed with Helm,
// so the chart values file — not the cluster — lists what can be deployed, where
// each image is pushed and which tag is live.
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

	opStart time.Time

	release   config.PSNReleaseConfig
	chartsDir string           // managed clone of the charts repo
	values    logic.HelmValues // what the release values declare

	namespace     string
	project       *config.PSNProjectConfig // per-namespace Dockerfile resolution override
	projectDir    string                   // managed clone of the project repo, "" = working copy
	projectURL    string                   // origin of the project repo
	projectBranch string                   // branch the project clone is aligned to
	scanRoot      string                   // root of a pending Dockerfile scan, set before psnDockerfileMissing
	svcByName     map[string]logic.HelmService
	selectedDeps  []string
	depIdx        int

	svc            logic.HelmService
	repo           string // registry + path, no tag
	oldTag         string
	newTag         string
	suggestedTag   string
	dockerfilePath string
	buildArgs      map[string]string
	buildArgQueue  []logic.DockerArg
	buildArgIdx    int
	depStart       time.Time
	svcLogStart    int // where the header of the service in hand sits in the log

	// deployed collects what has been built and pushed, for the single helm
	// upgrade at the end and for the sync that writes the tags back.
	deployed []psnDeployed
	syncErr  error

	// restartTargets are the Deployments still running the old image after the
	// upgrade, because their tag did not change.
	restartTargets []string

	results []DeployResult
}

// psnDeployed is one image built and pushed in this run. The elapsed time is
// recorded per service, when its push ends: the results are assembled later, in
// one go after the shared upgrade, and a clock read at that point would give
// every service the duration of the last one. It covers the service's own
// pipeline only — the single `helm upgrade` belongs to the whole run and is
// timed on its own line.
type psnDeployed struct {
	svc     logic.HelmService
	oldTag  string
	newTag  string
	elapsed time.Duration
}

// RunPSNWorkflow runs the PSN deploy pipeline as a single persistent BubbleTea
// program. Returns (results, cancelled, error).
func RunPSNWorkflow(cfg *config.Config, cluster config.PSNClusterConfig, release config.PSNReleaseConfig, dryRun, testUI bool) ([]DeployResult, bool, error) {
	label := "PSN — " + cluster.Name
	if testUI {
		label = "TEST-UI PSN"
	}
	SetStatus(label, cluster.AKSName)

	m := PSNWorkflowModel{
		cfg:     cfg,
		cluster: cluster,
		release: release,
		dryRun:  dryRun,
		testUI:  testUI,
		state:   psnChartsPrep,
		opStart: time.Now(),
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	ClearStatus()
	if err != nil {
		return nil, false, err
	}
	wf := final.(PSNWorkflowModel)
	// The final model is the only place that knows everything the closing table
	// says about the run: which namespace it reached, and what closed it.
	SetSummaryContext(cluster.Name+"  ·  "+wf.namespace, wf.summaryFooter())
	return wf.results, wf.cancelled, nil
}

// summaryFooter is the line under the table: one upgrade for every service, and
// the branch the tags were written back to.
func (m PSNWorkflowModel) summaryFooter() string {
	parts := []string{fmt.Sprintf("%d servizi", len(m.results))}
	if len(m.results) == 1 {
		parts[0] = "1 servizio"
	}
	if len(m.deployed) > 0 {
		parts = append(parts, "1 revisione helm")
	}
	switch {
	case m.syncErr != nil:
		parts = append(parts, "sync non riuscito")
	case len(m.deployed) > 0 && !m.dryRun && !m.testUI:
		parts = append(parts, "values su "+m.release.ChartsBranch)
	}
	return strings.Join(parts, "  ·  ")
}

// markRestarted records which services were redeployed under the same tag and
// had their pod recreated, so the summary can say so. Only a restart that
// actually completed counts.
func (m *PSNWorkflowModel) markRestarted() {
	for i := range m.results {
		if m.results[i].OldTag == m.results[i].NewTag {
			m.results[i].Restarted = true
		}
	}
}

// In test-ui nothing is read for real, so the services come from placeholders.
func psnFakeValues() logic.HelmValues {
	return logic.HelmValues{
		Namespace: "demo-ns-col",
		Services: []logic.HelmService{
			{Name: "webapp", Repository: "demoacr.azurecr.io/demo/webapp", Tag: "1.0.0",
				Keys: []logic.HelmImageKey{{Key: "webapp", SetKey: "webapp.image.tag", Deployment: "webapp"}}},
			{Name: "jboss-fe", Repository: "demoacr.azurecr.io/demo/jboss-fe", Tag: "2.1.3",
				Keys: []logic.HelmImageKey{{Key: "jbossFe", SetKey: "jbossFe.image.tag", Deployment: "jboss-fe"}}},
		},
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) Init() tea.Cmd {
	return func() tea.Msg { return psnStartMsg{} }
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
	if _, ok := msg.(psnStartMsg); ok {
		return m.start()
	}
	if cp, ok := msg.(psnChartsPrepDoneMsg); ok {
		return m.handleChartsPrepDone(cp)
	}
	if sy, ok := msg.(psnSyncDoneMsg); ok {
		elapsed := formatElapsed(time.Since(m.opStart))
		if sy.err != nil {
			m.log = append(m.log, logFailure("Sync del values", sy.err, elapsed)...)
			m.log = append(m.log, logWarn(m.release.ChartsBranch+
				" non riflette il deploy: al prossimo helm upgrade i tag tornerebbero indietro.")...)
		} else {
			m.log = append(m.log, logDone("Sync del values", m.release.ChartsBranch, elapsed))
		}
		m.syncErr = sy.err
		m.state = psnSummary
		return m, tea.Quit
	}
	if rp, ok := msg.(psnRepoPrepDoneMsg); ok {
		if rp.err != nil {
			// The declared branch may have been renamed: offer what the remote
			// actually publishes rather than failing outright.
			if errors.Is(rp.err, logic.ErrBranchNotFound) {
				m.log = append(m.log, WarnStyle.Render("  ⚠  "+rp.err.Error()))
				return m.enterProjectBranchLoading()
			}
			m.log = append(m.log, logFailure("Progetto Docker", rp.err, "")...)
			m.cancelled = true
			return m, tea.Quit
		}
		m.projectDir = rp.dir
		m.log = append(m.log, logDone("Progetto Docker", "origin/"+m.projectBranch,
			formatElapsed(time.Since(m.opStart))))
		return m.enterDepSelect()
	}
	if bl, ok := msg.(psnBranchesLoadedMsg); ok {
		if bl.err != nil || len(bl.branches) == 0 {
			m.log = append(m.log, ErrStyle.Render("  ✗  Nessun branch leggibile su "+m.projectURL))
			m.cancelled = true
			return m, tea.Quit
		}
		return m.enterProjectBranchSelect(bl.branches)
	}
	if done, ok := msg.(psnOpDoneMsg); ok {
		return m.handleOpDone(done)
	}
	return m.forwardToActive(msg)
}

func (m PSNWorkflowModel) forwardToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case psnChartsPrep, psnRepoPrep, psnBuilding, psnPushing, psnHelmDeploy, psnRestarting, psnSync:
		sm, cmd := m.spinner.Update(msg)
		m.spinner = sm.(spinnerModel)
		return m, cmd

	case psnBranchSelect, psnDockerfileList, psnDockerfileMissing, psnDeployError, psnRestartConfirm:
		sm, cmd := m.list.Update(msg)
		m.list = sm.(listModel)
		if m.list.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.list.done {
			switch m.state {
			case psnBranchSelect:
				return m.finishProjectBranchSelect()
			case psnDockerfileList:
				return m.finishDockerfile()
			case psnDockerfileMissing:
				return m.finishDockerfileMissing()
			case psnRestartConfirm:
				return m.finishRestartPrompt()
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

// ── Release selection ─────────────────────────────────────────────────────────

// start picks the release to deploy. A cluster hosts more than one, so with
// several configured the choice is explicit; with one it is implicit.
// start picks up from the release chosen before the Azure phase: nothing here
// needs the cluster, so choosing it earlier lets the header — and the namespace
// it names — sit above the whole run instead of half way down it.
func (m PSNWorkflowModel) start() (tea.Model, tea.Cmd) {
	m.namespace = m.release.Namespace
	if !m.testUI {
		m.project = m.cfg.PSN.ProjectForNamespace(m.release.Namespace)
	}
	return m.enterChartsPrep()
}

// ── Charts repo and values ────────────────────────────────────────────────────

// enterChartsPrep aligns the managed clone of the charts repo to the branch the
// release is deployed from. Chart and values travel together on that branch, and
// the same values file differs between branches, so the branch is not a detail.
func (m PSNWorkflowModel) enterChartsPrep() (tea.Model, tea.Cmd) {
	if m.testUI {
		m.values = psnFakeValues()
		return m.enterDepSelect()
	}

	helmRoot := m.cfg.Config.HelmRootPath
	if helmRoot == "" {
		m.log = append(m.log, ErrStyle.Render("  ✗  helm_root_path non configurato: serve per risolvere il repo dei chart"))
		m.cancelled = true
		return m, tea.Quit
	}
	remoteURL, err := logic.GitRemoteURL(helmRoot)
	if err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  "+err.Error()))
		m.cancelled = true
		return m, tea.Quit
	}

	m.state = psnChartsPrep
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Allineamento repo chart (" + m.release.ChartsBranch + ")")
	branch := m.release.ChartsBranch
	reposRoot := config.GetReposRoot()
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		dir, err := logic.EnsureRepo(remoteURL, branch, reposRoot, nil)
		return psnChartsPrepDoneMsg{dir: dir, err: err}
	})
}

func (m PSNWorkflowModel) handleChartsPrepDone(msg psnChartsPrepDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  Allineamento del repo chart non riuscito: "+msg.err.Error()))
		m.cancelled = true
		return m, tea.Quit
	}
	m.chartsDir = msg.dir
	elapsed := formatElapsed(time.Since(m.opStart))

	valuesPath := filepath.Join(m.chartsDir, m.release.Values)
	values, err := logic.ReadHelmValues(valuesPath)
	if err != nil {
		m.log = append(m.log, logFailure("Chart", err, elapsed)...)
		m.cancelled = true
		return m, tea.Quit
	}
	m.values = values

	// The version is read before the step is logged, so it can ride on the same
	// line instead of trailing it as a second fact about the same thing.
	note := "origin/" + m.release.ChartsBranch
	if v, err := logic.ReadChartVersion(filepath.Join(m.chartsDir, m.release.Chart)); err == nil {
		note += " · " + v
	}
	m.log = append(m.log, logDone("Chart", note, elapsed))

	// The values declare their own namespace: a mismatch means the release is
	// pointed at the wrong file, which would deploy into the wrong place.
	if values.Namespace != "" && values.Namespace != m.namespace {
		m.log = append(m.log, logWarn(fmt.Sprintf(
			"Il values dichiara namespace %q, la configurazione %q", values.Namespace, m.namespace))...)
	}

	if m.project != nil {
		// With a branch declared the configured path only supplies the remote
		// URL: the build reads from the managed clone aligned right after.
		if want := m.project.ExpectedBranch(m.cluster); want != "" {
			return m.enterProjectRepoPrep(want)
		}
		m.log = append(m.log, logInfo("Progetto", shortPath(m.project.DockerRoot)))
	}
	return m.enterDepSelect()
}

// ── Project repo preparation ──────────────────────────────────────────────────

// enterProjectRepoPrep realigns the managed clone of the project repo to the
// branch this environment builds from: the branch decides what ends up inside
// the images, and colleagues push to it without going through hub-cli.
func (m PSNWorkflowModel) enterProjectRepoPrep(want string) (tea.Model, tea.Cmd) {
	if m.dryRun || m.testUI {
		if m.dryRun {
			m.log = append(m.log, wfDryRunLine(fmt.Sprintf(
				"git fetch + checkout di %s su %s (repo gestito)", m.project.DockerRoot, want)))
		}
		return m.enterDepSelect()
	}

	remoteURL, err := logic.GitRemoteURL(m.project.DockerRoot)
	if err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  "+err.Error()))
		m.cancelled = true
		return m, tea.Quit
	}
	m.projectURL = remoteURL
	return m.alignProjectRepo(want)
}

// alignProjectRepo aligns the managed clone to branch.
func (m PSNWorkflowModel) alignProjectRepo(branch string) (tea.Model, tea.Cmd) {
	m.projectBranch = branch
	m.state = psnRepoPrep
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Allineamento repo progetto (" + branch + ")")
	remoteURL := m.projectURL
	reposRoot := config.GetReposRoot()
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		dir, err := logic.EnsureRepo(remoteURL, branch, reposRoot, nil)
		return psnRepoPrepDoneMsg{dir: dir, err: err}
	})
}

// enterProjectBranchLoading runs only when the branch declared in the seed is
// missing from the remote, so the run can continue on a branch that exists
// instead of stopping on a stale configuration.
func (m PSNWorkflowModel) enterProjectBranchLoading() (tea.Model, tea.Cmd) {
	m.state = psnRepoPrep
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Lettura branch del progetto")
	remoteURL := m.projectURL
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		branches, err := logic.ListRemoteBranches(remoteURL)
		return psnBranchesLoadedMsg{branches: branches, err: err}
	})
}

func (m PSNWorkflowModel) enterProjectBranchSelect(branches []string) (tea.Model, tea.Cmd) {
	items := make([]Item, len(branches))
	for i, b := range branches {
		items[i] = Item{Value: b, Label: b}
	}

	m.state = psnBranchSelect
	m.list = listModel{
		title: "Branch del progetto Docker per " + m.cluster.Name,
		items: items,
		width: m.width,
	}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishProjectBranchSelect() (tea.Model, tea.Cmd) {
	return m.alignProjectRepo(m.list.selected)
}

// projectRoot is the directory the build reads from: the managed clone when the
// project declares a branch, the working copy otherwise.
func (m PSNWorkflowModel) projectRoot() string {
	if m.projectDir != "" {
		return m.projectDir
	}
	return m.project.DockerRoot
}

// ── Service selection ─────────────────────────────────────────────────────────

// enterDepSelect offers the services declared in the values file. With Helm the
// values are the source of truth: they hold the tag that will actually be
// applied, which is the one worth incrementing.
func (m PSNWorkflowModel) enterDepSelect() (tea.Model, tea.Cmd) {
	if len(m.values.Services) == 0 {
		m.log = append(m.log, ErrStyle.Render("  ✗  Nessun servizio con immagine in "+m.release.Values))
		m.cancelled = true
		return m, tea.Quit
	}

	m.svcByName = make(map[string]logic.HelmService, len(m.values.Services))
	items := make([]Item, 0, len(m.values.Services))
	for _, svc := range m.values.Services {
		m.svcByName[svc.Name] = svc
		desc := svc.Tag
		if len(svc.Keys) > 1 {
			desc += fmt.Sprintf("  ·  %d riferimenti", len(svc.Keys))
		}
		if !svc.TagsAgree() {
			desc += "  ·  tag divergenti nel values"
		}
		items = append(items, Item{Value: svc.Name, Label: svc.Name, Desc: desc})
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
	if len(selected) == 0 {
		m.cancelled = true
		return m, tea.Quit
	}
	m.selectedDeps = selected
	m.depIdx = 0
	return m.startNextDeployment()
}

// ── Per-service pipeline ──────────────────────────────────────────────────────

// startNextDeployment builds and pushes one image at a time; the release is
// upgraded once, at the end, with every new tag in the same revision.
func (m PSNWorkflowModel) startNextDeployment() (tea.Model, tea.Cmd) {
	if m.depIdx >= len(m.selectedDeps) {
		return m.enterHelmDeploy()
	}

	name := m.selectedDeps[m.depIdx]
	m.svc = m.svcByName[name]
	m.repo = m.svc.Repository
	m.oldTag = m.svc.Tag
	m.newTag = ""
	m.dockerfilePath = ""
	m.buildArgs = make(map[string]string)
	m.buildArgQueue = nil
	m.buildArgIdx = 0
	m.depStart = time.Now()

	SetStatus(name, m.cluster.Name)

	// The service opens with its header straight away, the tag half pending
	// until chosen: the question about the tag is then asked under the section
	// it belongs to, not above it.
	m.svcLogStart = len(m.log)
	m.log = append(m.log, m.serviceHeader())
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

// serviceHeader renders the header of the service in hand. Its tag half stays
// pending until the tag is chosen.
func (m PSNWorkflowModel) serviceHeader() string {
	return logServiceHeader(m.depIdx+1, len(m.selectedDeps), m.svc.Name, m.oldTag, m.newTag)
}

func (m PSNWorkflowModel) finishTagInput() (tea.Model, tea.Cmd) {
	val := m.input.textInput.Value()
	if val == "" {
		val = m.suggestedTag
	}
	m.newTag = val

	m.log[m.svcLogStart] = m.serviceHeader()

	// Redeploying the same tag overwrites the image on ACR, but helm renders an
	// identical manifest: Kubernetes sees no change and never recreates the pod,
	// so the old image keeps running. imagePullPolicy: Always does not help —
	// it governs pod creation, not whether a pod is recreated.
	if m.newTag == m.oldTag {
		m.log = append(m.log, logWarn(
			"Il manifest non cambia: il pod non riparte da solo.",
			"A fine deploy hub-cli propone il riavvio.")...)
	}
	if !m.svc.TagsAgree() {
		m.log = append(m.log, logWarn(
			"Il values dichiara tag diversi per la stessa immagine.",
			"Verranno allineati tutti al nuovo tag.")...)
	}
	m.log = append(m.log, logInfo("Immagine", m.repo+":"+m.oldTag))

	return m.resolveDockerfile()
}

// ── Dockerfile resolve ────────────────────────────────────────────────────────

// resolveDockerfile finds the Dockerfile for the current deployment. With a
// per-namespace project configured, resolution happens inside its docker_root;
// otherwise via the psn.deployments → local service mapping, then by scanning
// docker_root_path.
func (m PSNWorkflowModel) resolveDockerfile() (tea.Model, tea.Cmd) {
	if m.testUI {
		m.log = append(m.log, logInfo("Dockerfile", "(simulato)"))
		return m.enterBuild()
	}

	if m.project != nil {
		return m.resolveProjectDockerfile()
	}

	if svcName, ok := m.cfg.PSN.Deployments[strings.ToLower(m.svc.Name)]; ok && svcName != "" {
		if svc, found := m.cfg.Services[svcName]; found && svc.DockerfileSubpath != "" {
			full := filepath.Join(m.cfg.Config.DockerRootPath, svc.DockerfileSubpath)
			if abs, err := filepath.Abs(full); err == nil {
				if _, err := os.Stat(abs); err == nil {
					m.dockerfilePath = abs
					m.log = append(m.log, logInfo("Dockerfile", shortPath(abs)+DimStyle.Render("  · da "+svcName)))
					return m.afterDockerfile()
				}
				m.log = append(m.log, logWarn("Dockerfile del servizio mappato non trovato", shortPath(abs))...)
				return m.enterDockerfileMissing(m.cfg.Config.DockerRootPath)
			}
		} else {
			m.log = append(m.log, logWarn(fmt.Sprintf(
				"Mapping %q → servizio %q non risolvibile in config", m.svc.Name, svcName))...)
			return m.enterDockerfileMissing(m.cfg.Config.DockerRootPath)
		}
	}

	return m.enterDockerfileScan(m.cfg.Config.DockerRootPath)
}

// enterDockerfileMissing asks what to do when the configured resolution fails.
// The choice does not affect the destination — the image goes to the ACR
// repository this deployment already runs — so cancelling comes first.
func (m PSNWorkflowModel) enterDockerfileMissing(scanRoot string) (tea.Model, tea.Cmd) {
	m.scanRoot = scanRoot
	m.state = psnDockerfileMissing
	m.list = listModel{
		title: "Dockerfile di " + m.svc.Name + " non risolto",
		items: []Item{
			{Value: "cancel", Label: "Annulla", Desc: "interrompe il deploy senza buildare"},
			{Value: "scan", Label: "Cerca un Dockerfile", Desc: "l'immagine verrà pushata su " + m.repo},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishDockerfileMissing() (tea.Model, tea.Cmd) {
	if m.list.selected != "scan" {
		m.log = append(m.log, ErrStyle.Render("  ✗  Deploy annullato: Dockerfile di "+m.svc.Name+" non risolto"))
		m.cancelled = true
		return m, tea.Quit
	}
	return m.enterDockerfileScan(m.scanRoot)
}

// enterDockerfileScan offers what it finds: a discovered Dockerfile is never
// used without being shown.
func (m PSNWorkflowModel) enterDockerfileScan(root string) (tea.Model, tea.Cmd) {
	m.log = append(m.log, logInfo("Scansione", shortPath(root)))

	files, err := logic.FindDockerfiles(root)
	if err != nil || len(files) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun Dockerfile trovato in %s", root)))
		return m, tea.Quit
	}

	m.state = psnDockerfileList
	m.list = listModel{
		title: wfDockerfileListTitle(m.svc.Name, m.repo),
		items: dockerfileItems(files, root),
		width: m.width,
	}
	return m, m.list.Init()
}

// resolveProjectDockerfile resolves inside the project's docker_root:
// explicit mapping → convention "<deployment>/Dockerfile" → full scan.
func (m PSNWorkflowModel) resolveProjectDockerfile() (tea.Model, tea.Cmd) {
	root := m.projectRoot()

	if rel, ok := m.project.Deployments[strings.ToLower(m.svc.Name)]; ok && rel != "" {
		full := filepath.Join(root, rel)
		if _, err := os.Stat(full); err == nil {
			m.dockerfilePath = full
			m.log = append(m.log, logInfo("Dockerfile", shortPath(full)))
			return m.afterDockerfile()
		}
		m.log = append(m.log, logWarn("Dockerfile mappato non trovato", shortPath(full))...)
		return m.enterDockerfileMissing(root)
	}

	conventional := filepath.Join(root, m.svc.Name, "Dockerfile")
	if _, err := os.Stat(conventional); err == nil {
		m.dockerfilePath = conventional
		m.log = append(m.log, logInfo("Dockerfile", shortPath(conventional)))
		return m.afterDockerfile()
	}

	return m.enterDockerfileScan(root)
}

func (m PSNWorkflowModel) finishDockerfile() (tea.Model, tea.Cmd) {
	m.dockerfilePath = m.list.selected
	m.log = append(m.log, logInfo("Dockerfile", shortPath(m.dockerfilePath)))
	return m.afterDockerfile()
}

func (m PSNWorkflowModel) afterDockerfile() (tea.Model, tea.Cmd) {
	if dockerArgs, err := logic.ParseDockerfileArgs(m.dockerfilePath); err == nil && len(dockerArgs) > 0 {
		m.log = append(m.log, logInfo("Build ARG", fmt.Sprintf("%d rilevati nel Dockerfile", len(dockerArgs))))
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
	// A blank line between what the service is and what is being done to it:
	// the facts above are context, everything below is the pipeline running.
	m.log = append(m.log, "")

	if m.dryRun {
		buildArgStr := ""
		for k, v := range m.buildArgs {
			buildArgStr += fmt.Sprintf(" --build-arg %s=%s", k, v)
		}
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker build --no-cache -t %s:%s -f %s%s %s",
			m.repo, m.newTag, m.dockerfilePath, buildArgStr, filepath.Dir(m.dockerfilePath))))
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf("docker push %s:%s", m.repo, m.newTag)))
		m.results = append(m.results, DeployResult{Service: m.svc.Name, OldTag: m.oldTag, NewTag: m.newTag, Skipped: true})
		// Recorded in dry-run too: without it the run would have nothing to
		// deploy and would skip straight past the helm upgrade and the sync,
		// which are the two commands worth previewing.
		m.deployed = append(m.deployed, psnDeployed{
			svc: m.svc, oldTag: m.oldTag, newTag: m.newTag, elapsed: time.Since(m.depStart)})
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

// ── Deploy: helm upgrade ──────────────────────────────────────────────────────

// enterHelmDeploy upgrades the release once, carrying every new tag in the same
// revision. One upgrade per service would produce a revision each and, on a
// partial failure, a release updated halfway.
func (m PSNWorkflowModel) enterHelmDeploy() (tea.Model, tea.Cmd) {
	if len(m.deployed) == 0 {
		m.state = psnSummary
		return m, tea.Quit
	}

	setArgs := m.helmSetArgs()
	chartDir := filepath.Join(m.chartsDir, m.release.Chart)
	valuesPath := filepath.Join(m.chartsDir, m.release.Values)

	// The upgrade, the restarts and the sync belong to the release, not to the
	// service that happened to be built last. Without a section of their own
	// they used to be logged under the final service, and read as its steps.
	m.log = append(m.log, logSection("Rilascio", m.release.Name))

	if m.dryRun {
		m.log = append(m.log, wfDryRunLine(logic.HelmCommandLine(
			m.release.Name, chartDir, valuesPath, m.namespace, setArgs)))
		targets, _ := psnRestartTargets(m.deployed)
		for _, name := range targets {
			m.log = append(m.log, wfDryRunLine(logic.RolloutRestartCommandLine(name, m.namespace)))
		}
		return m.enterSync()
	}

	m.state = psnHelmDeploy
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("helm upgrade " + m.release.Name)
	release, namespace, testUI := m.release.Name, m.namespace, m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1500 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.HelmDeployChart(release, chartDir, valuesPath, namespace, setArgs, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// helmSetArgs is one --set per values key of every image bumped. An image
// referenced twice gets both its keys, or the two references would drift apart.
func (m PSNWorkflowModel) helmSetArgs() []string {
	var args []string
	for _, d := range m.deployed {
		for _, k := range d.svc.Keys {
			if k.ImagePath != "" {
				args = append(args, fmt.Sprintf("%s=%s:%s", k.SetKey, k.ImagePath, d.newTag))
			} else {
				args = append(args, fmt.Sprintf("%s=%s", k.SetKey, d.newTag))
			}
		}
	}
	return args
}

// ── Deploy error recovery ─────────────────────────────────────────────────────

func (m PSNWorkflowModel) enterDeployError() (tea.Model, tea.Cmd) {
	m.state = psnDeployError
	m.list = listModel{
		errorTone: true,
		title:     "Cosa vuoi fare?",
		items: []Item{
			{Value: "retry", Label: "Riprova", Desc: "riesegue helm upgrade"},
			{Value: "cancel", Label: "Annulla", Desc: "il release resta invariato, le immagini restano su ACR"},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishDeployError() (tea.Model, tea.Cmd) {
	if m.list.selected == "retry" {
		return m.enterHelmDeploy()
	}
	m.cancelled = true
	return m, tea.Quit
}

// ── Restart dei servizi a tag invariato ───────────────────────────────────────

// psnRestartTargets lists the Deployments that came out of the upgrade still
// running the old image — those whose tag did not change, where helm rendered an
// identical manifest — plus the values keys that declare no Deployment name and
// so cannot be restarted.
func psnRestartTargets(deployed []psnDeployed) (targets []string, unnamed []string) {
	for _, d := range deployed {
		if d.newTag != d.oldTag {
			continue
		}
		names, missing := d.svc.Deployments()
		for _, name := range names {
			if !slices.Contains(targets, name) {
				targets = append(targets, name)
			}
		}
		unnamed = append(unnamed, missing...)
	}
	return targets, unnamed
}

// enterRestartPrompt asks before restarting: with one replica and the chart's
// default rolling update the new pod starts before the old one goes away, so two
// instances overlap for a few dozen seconds. That is not a surprise to spring on
// a deploy in progress.
func (m PSNWorkflowModel) enterRestartPrompt() (tea.Model, tea.Cmd) {
	targets, unnamed := psnRestartTargets(m.deployed)
	for _, key := range unnamed {
		m.log = append(m.log, WarnStyle.Render(fmt.Sprintf(
			"  ⚠  %s: tag invariato, ma il values non dichiara un nome di deployment — riavvio non possibile", key)))
	}
	if len(targets) == 0 {
		return m.enterSync()
	}
	m.restartTargets = targets

	m.state = psnRestartConfirm
	m.list = listModel{
		title: "Tag invariato per " + strings.Join(targets, ", ") + " — riavviare?",
		items: []Item{
			{Value: "restart", Label: "Riavvia",
				Desc: "rollout restart; per qualche decina di secondi girano due istanze"},
			{Value: "skip", Label: "Salta", Desc: "il pod continua a girare l'immagine precedente"},
		},
		width: m.width,
	}
	return m, m.list.Init()
}

func (m PSNWorkflowModel) finishRestartPrompt() (tea.Model, tea.Cmd) {
	if m.list.selected != "restart" {
		m.log = append(m.log, WarnStyle.Render(
			"  ⚠  Riavvio saltato: "+strings.Join(m.restartTargets, ", ")+" gira ancora l'immagine precedente"))
		return m.enterSync()
	}
	return m.enterRestart()
}

func (m PSNWorkflowModel) enterRestart() (tea.Model, tea.Cmd) {
	m.state = psnRestarting
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Rollout restart di " + strings.Join(m.restartTargets, ", "))

	targets, namespace, testUI := m.restartTargets, m.namespace, m.testUI
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		if testUI {
			time.Sleep(1500 * time.Millisecond)
			return psnOpDoneMsg{}
		}
		var buf bytes.Buffer
		err := logic.RestartDeployments(targets, namespace, &buf)
		return psnOpDoneMsg{err: err, output: buf.Bytes()}
	})
}

// ── Sync: il tag torna nel values ─────────────────────────────────────────────

// enterSync writes the deployed tags into the values file and pushes them. The
// upgrade carried them as --set overrides only: without this the next
// `helm upgrade` run by anybody from that branch would put the old tags back.
func (m PSNWorkflowModel) enterSync() (tea.Model, tea.Cmd) {
	if len(m.deployed) == 0 || m.testUI {
		m.state = psnSummary
		return m, tea.Quit
	}
	if m.dryRun {
		m.log = append(m.log, wfDryRunLine(fmt.Sprintf(
			"aggiornamento di %s + commit e push su %s", m.release.Values, m.release.ChartsBranch)))
		m.log = append(m.log, "\n"+SecondaryStyle.Render(
			"  ◆  DRY-RUN  —  "+psnDryRunRecap(m.deployed)+"  (nulla è stato eseguito)"))
		m.state = psnSummary
		return m, tea.Quit
	}

	remoteURL, err := logic.GitRemoteURL(m.cfg.Config.HelmRootPath)
	if err != nil {
		m.log = append(m.log, ErrStyle.Render("  ✗  Sync non eseguito: "+err.Error()))
		m.syncErr = err
		m.state = psnSummary
		return m, tea.Quit
	}

	m.state = psnSync
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Sync del values su " + m.release.ChartsBranch)

	deployed := m.deployed
	valuesRel := m.release.Values
	branch := m.release.ChartsBranch
	reposRoot := config.GetReposRoot()
	message := logic.DeployCommitMessageFor(psnDeployedServices(deployed))

	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		err := logic.SyncToBranch(remoteURL, branch, reposRoot, message,
			func(dir string) ([]string, error) {
				path := filepath.Join(dir, valuesRel)
				for _, d := range deployed {
					for _, k := range d.svc.Keys {
						if err := logic.UpdateHelmValuesTag(path, k.SetKey, d.newTag, k.ImagePath); err != nil {
							return nil, fmt.Errorf("%s: %w", d.svc.Name, err)
						}
					}
				}
				return []string{valuesRel}, nil
			}, nil)

		return psnSyncDoneMsg{err: err}
	})
}

// psnDryRunRecap lists what a real run would have deployed, in one line.
func psnDryRunRecap(deployed []psnDeployed) string {
	parts := make([]string, 0, len(deployed))
	for _, d := range deployed {
		parts = append(parts, fmt.Sprintf("%s %s → %s", d.svc.Name, d.oldTag, d.newTag))
	}
	return strings.Join(parts, ", ")
}

// appendOutput adds captured command output to the log, when there is any.
func appendOutput(log []string, out []byte) []string {
	if rendered := logOutput(out); rendered != "" {
		return append(log, rendered)
	}
	return log
}

// psnUpgradeNote says what the single upgrade covered: one revision for every
// service of the run, which is the reason it is one command and not several.
func psnUpgradeNote(deployed []psnDeployed) string {
	if len(deployed) == 1 {
		return "1 servizio · 1 revisione"
	}
	return fmt.Sprintf("%d servizi · 1 revisione", len(deployed))
}

func psnDeployedServices(deployed []psnDeployed) []logic.DeployedService {
	out := make([]logic.DeployedService, 0, len(deployed))
	for _, d := range deployed {
		out = append(out, logic.DeployedService{Name: d.svc.Name, Tag: d.newTag})
	}
	return out
}

// ── handleOpDone ──────────────────────────────────────────────────────────────

func (m PSNWorkflowModel) handleOpDone(msg psnOpDoneMsg) (tea.Model, tea.Cmd) {
	elapsed := formatElapsed(time.Since(m.opStart))

	if msg.err != nil {
		// A failure keeps the grid: same columns, same place, only the glyph and
		// the colour change. What the command printed is indented underneath, so
		// it reads as belonging to the step that produced it.
		switch m.state {
		case psnBuilding:
			m.log = append(m.log, logFailure("Build", msg.err, elapsed)...)
			m.log = appendOutput(m.log, msg.output)
			return m, tea.Quit
		case psnPushing:
			m.log = append(m.log, logFailure("Push", msg.err, elapsed)...)
			m.log = appendOutput(m.log, msg.output)
			return m, tea.Quit
		case psnHelmDeploy:
			m.log = append(m.log, logFailure("helm upgrade", msg.err, elapsed)...)
			m.log = appendOutput(m.log, msg.output)
			return m.enterDeployError()
		case psnRestarting:
			// The upgrade already landed: the values still have to be synced,
			// or the next run from that branch would put the old tags back.
			m.log = append(m.log, logFailure("Riavvio", msg.err, elapsed)...)
			m.log = appendOutput(m.log, msg.output)
			m.log = append(m.log, logWarn(
				"Il pod potrebbe girare ancora l'immagine precedente: verificare sul cluster.")...)
			return m.enterSync()
		}
	}

	switch m.state {
	case psnBuilding:
		m.log = append(m.log, logDone("Build", "", elapsed))
		return m.enterPush()

	case psnPushing:
		m.log = append(m.log, logDone("Push", "", elapsed))
		m.deployed = append(m.deployed, psnDeployed{
			svc: m.svc, oldTag: m.oldTag, newTag: m.newTag, elapsed: time.Since(m.depStart)})
		m.depIdx++
		return m.startNextDeployment()

	case psnHelmDeploy:
		m.log = append(m.log, logDone("helm upgrade", psnUpgradeNote(m.deployed), elapsed))
		for _, d := range m.deployed {
			m.results = append(m.results, DeployResult{
				Service: d.svc.Name,
				OldTag:  d.oldTag,
				NewTag:  d.newTag,
				Elapsed: d.elapsed,
			})
		}
		return m.enterRestartPrompt()

	case psnRestarting:
		m.log = append(m.log, logStep("↻", CursorStyle,
			"Riavvio "+strings.Join(m.restartTargets, ", "), "tag invariato", elapsed))
		m.markRestarted()
		return m.enterSync()
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
	case psnChartsPrep, psnRepoPrep, psnBuilding, psnPushing, psnHelmDeploy, psnRestarting, psnSync:
		sb.WriteString(logRunning(m.spinner.spinner.View(), m.spinner.label,
			formatElapsed(time.Since(m.opStart))) + "\n")
	// A question stands off the log it interrupts, instead of continuing it.
	case psnBranchSelect, psnDockerfileList, psnDockerfileMissing, psnDeployError, psnRestartConfirm:
		sb.WriteString("\n" + m.list.View().Content)
	case psnDepSelect:
		sb.WriteString("\n" + m.multisel.View().Content)
	case psnTagInput, psnBuildArg:
		sb.WriteString("\n" + m.input.View().Content)
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
	frame := m.spinner.spinner.View()
	inPipeline := m.state >= psnTagInput && m.state < psnHelmDeploy

	// The Azure phase always completes before the TUI starts.
	tabs := []string{trackerTab(trackDone, "Azure", frame)}

	// Named for what this phase does, not for what used to be chosen here: the
	// release is picked before the TUI starts, and what is left is the chart repo
	// being aligned and its values read.
	if m.state == psnChartsPrep {
		tabs = append(tabs, trackerTab(trackSpinning, "Chart", frame))
	} else {
		tabs = append(tabs, trackerTab(trackDone, "Chart", frame))
	}

	switch {
	case m.state < psnDepSelect:
		tabs = append(tabs, trackerTab(trackPending, "Servizi", frame))
	case m.state == psnDepSelect:
		tabs = append(tabs, trackerTab(trackInteractive, "Servizi", frame))
	default:
		tabs = append(tabs, trackerTab(trackDone, "Servizi", frame))
	}

	pipeline := fmt.Sprintf("Pipeline [%d/%d]", min(m.depIdx+1, len(m.selectedDeps)), len(m.selectedDeps))
	switch {
	case m.state < psnTagInput:
		tabs = append(tabs, trackerTab(trackPending, "Pipeline", frame))
	case inPipeline:
		tabs = append(tabs, trackerTab(trackInteractive, pipeline, frame))
	default:
		tabs = append(tabs, trackerTab(trackDone, pipeline, frame))
	}

	// Rilascio and Sync happen once for the whole run, so they belong to this
	// row and not to the per-service one below.
	switch {
	case m.state >= psnSync:
		tabs = append(tabs, trackerTab(trackDone, "Rilascio", frame))
	case m.state == psnDeployError:
		tabs = append(tabs, trackerTab(trackFailed, "Rilascio", frame))
	case m.state == psnRestartConfirm:
		tabs = append(tabs, trackerTab(trackInteractive, "Rilascio", frame))
	case m.state == psnHelmDeploy || m.state == psnRestarting:
		tabs = append(tabs, trackerTab(trackSpinning, "Rilascio", frame))
	default:
		tabs = append(tabs, trackerTab(trackPending, "Rilascio", frame))
	}

	switch {
	case m.state >= psnSummary:
		tabs = append(tabs, trackerTab(trackDone, "Sync", frame))
	case m.state == psnSync:
		tabs = append(tabs, trackerTab(trackSpinning, "Sync", frame))
	default:
		tabs = append(tabs, trackerTab(trackPending, "Sync", frame))
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(trackerRow(tabs) + "\n")
	sb.WriteString(trackerRule(m.width) + "\n")
	if inPipeline {
		sb.WriteString(m.renderPipelineStages() + "\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderPipelineStages is the row of the service being deployed. No durations
// here: every stage logs its own two lines below, and repeating it would say
// the same number twice.
func (m PSNWorkflowModel) renderPipelineStages() string {
	frame := m.spinner.spinner.View()

	config := trackPending
	switch {
	case m.state >= psnBuilding:
		config = trackDone
	case m.state >= psnTagInput:
		config = trackInteractive
	}

	async := func(activeAt, doneAt psnState) trackerState {
		switch {
		case m.state >= doneAt:
			return trackDone
		case m.state == activeAt:
			return trackSpinning
		}
		return trackPending
	}

	return stageRow(m.svc.Name, []string{
		trackerTab(config, "Config", frame),
		trackerTab(async(psnBuilding, psnPushing), "Build", frame),
		trackerTab(async(psnPushing, psnHelmDeploy), "Push", frame),
	})
}
