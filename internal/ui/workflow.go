package ui

import (
	"bytes"
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
	wfServiceSelect
	wfSvcTagSync
	wfSvcDockerfile
	wfSvcTagInput
	wfSvcBuildArg
	wfSvcBuilding
	wfSvcPushing
	wfSvcHelm
	wfSvcHelmError
	wfSvcRollback
	wfSvcPostSync
	wfSummary
)

// ── Messages ──────────────────────────────────────────────────────────────────

type wfOpDoneMsg struct {
	err    error
	output []byte
}

type wfPostSyncDoneMsg struct {
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

	results []DeployResult
}

// RunWorkflow runs the full deploy pipeline as a single persistent BubbleTea program.
// Returns (results, cancelled, error).
func RunWorkflow(cfg *config.Config, dryRun, testUI bool) ([]DeployResult, bool, error) {
	label := "LOCAL DEPLOY"
	if testUI {
		label = "TEST-UI"
	}
	SetStatus(label, cfg.Config.ECRRegion)

	m := WorkflowModel{
		cfg:     cfg,
		dryRun:  dryRun,
		testUI:  testUI,
		state:   wfECRLogin,
		spinner: newSpinnerModel("ECR Login"),
		opStart: time.Now(),
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	ClearStatus()
	if err != nil {
		return nil, false, err
	}
	wf := final.(WorkflowModel)
	return wf.results, wf.cancelled, nil
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
	if ps, ok := msg.(wfPostSyncDoneMsg); ok {
		m.log = append(m.log, ps.lines...)
		m.results = append(m.results, DeployResult{
			Service: m.svcName,
			OldTag:  m.oldTag,
			NewTag:  m.newTag,
			Elapsed: time.Since(m.svcStart),
		})
		m.svcIdx++
		return m.startNextService()
	}
	return m.forwardToActive(msg)
}

func (m WorkflowModel) forwardToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case wfECRLogin, wfSvcBuilding, wfSvcPushing, wfSvcHelm, wfSvcRollback, wfSvcPostSync:
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

	case wfSvcDockerfile, wfSvcHelmError:
		sm, cmd := m.list.Update(msg)
		m.list = sm.(listModel)
		if m.list.quit {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.list.done {
			if m.state == wfSvcDockerfile {
				return m.finishDockerfile()
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
		return m.enterServiceSelect()

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
		return m.enterPostSync()

	case wfSvcRollback:
		m.log = append(m.log, WarnStyle.Render("  ⚠  Rollback completato"))
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
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
		return m.enterSummary()
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
	cfg := m.cfg
	svcName := m.svcName

	if svc.DockerfileSubpath != "" {
		full := filepath.Join(cfg.Config.DockerRootPath, svc.DockerfileSubpath)
		abs, err := filepath.Abs(full)
		if err == nil {
			if _, err := os.Stat(abs); err == nil {
				m.dockerfilePath = abs
				m.discovered = false
				m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+abs))
				return m.enterTagInput()
			}
			m.log = append(m.log, WarnStyle.Render("  ⚠  Dockerfile configurato non trovato: "+abs))
		}
	}

	searchRoot := cfg.Config.DockerRootPath
	if prefix := wfProjectPrefix(svcName); prefix != "" {
		projectDir := filepath.Join(cfg.Config.DockerRootPath, prefix)
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			searchRoot = projectDir
		}
	}
	m.log = append(m.log, DimStyle.Render(fmt.Sprintf("  ·  Scansione Dockerfile in %s...", searchRoot)))

	files, err := logic.FindDockerfiles(searchRoot)
	if err != nil || len(files) == 0 && searchRoot != cfg.Config.DockerRootPath {
		files, _ = logic.FindDockerfiles(cfg.Config.DockerRootPath)
	}

	if len(files) == 0 {
		m.log = append(m.log, ErrStyle.Render(fmt.Sprintf("  ✗  Nessun Dockerfile trovato in %s", cfg.Config.DockerRootPath)))
		return m, tea.Quit
	}
	if len(files) == 1 {
		m.dockerfilePath = files[0]
		m.discovered = true
		m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+files[0]))
		return m.enterTagInput()
	}
	return m.enterDockerfileList(files)
}

func (m WorkflowModel) enterDockerfileList(files []string) (tea.Model, tea.Cmd) {
	m.state = wfSvcDockerfile
	items := make([]Item, len(files))
	for i, f := range files {
		items[i] = Item{Value: f, Label: f}
	}
	m.list = listModel{title: "Seleziona Dockerfile", items: items, width: m.width}
	return m, m.list.Init()
}

func (m WorkflowModel) finishDockerfile() (tea.Model, tea.Cmd) {
	m.dockerfilePath = m.list.selected
	m.discovered = true
	m.log = append(m.log, DimStyle.Render("  ·  Dockerfile: "+m.dockerfilePath))
	return m.enterTagInput()
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

	if m.discovered && !m.testUI && m.cfg.Config.DockerRootPath != "" {
		rel, relErr := filepath.Rel(m.cfg.Config.DockerRootPath, m.dockerfilePath)
		if relErr != nil {
			rel = m.dockerfilePath
		}
		if err := config.UpdateServiceDockerfilePath(m.svcName, rel); err != nil {
			m.log = append(m.log, WarnStyle.Render("  ⚠  impossibile salvare path Dockerfile: "+err.Error()))
		} else {
			m.log = append(m.log, SuccessStyle.Render("  ✓  Path Dockerfile salvato."))
		}
	}

	content := fmt.Sprintf("  %s    %s  →  %s  ",
		SelectedItemStyle.Render(m.svcName),
		DimStyle.Render(m.oldTag),
		SuccessStyle.Render(m.newTag),
	)
	m.log = append(m.log, "\n"+BoxStyle.Render(content))

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
	m.valuesPath = filepath.Join(m.cfg.Config.HelmRootPath, m.svc.HelmValuesPath)
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

func (m WorkflowModel) enterPostSync() (tea.Model, tea.Cmd) {
	helmRoot := config.GetHelmRootPath()
	dockerRoot := config.GetDockerRootPath()
	hasHelm := helmRoot != "" && m.svc.HelmValuesPath != "" && m.svc.HelmSetKey != ""
	hasDocker := dockerRoot != "" && m.svc.K8sManifestPath != "" && m.svc.K8sImageRef != ""

	if !hasHelm && !hasDocker {
		return m.finishService()
	}

	m.state = wfSvcPostSync
	m.opStart = time.Now()
	m.spinner = newSpinnerModel("Aggiornamento repo")
	cfg := m.cfg
	svc := m.svc
	svcName := m.svcName
	newTag := m.newTag
	valuesPath := m.valuesPath
	return m, tea.Batch(m.spinner.Init(), func() tea.Msg {
		lines := wfRunPostDeploySync(cfg, svc, svcName, newTag, valuesPath)
		return wfPostSyncDoneMsg{lines: lines}
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
	case wfECRLogin, wfSvcBuilding, wfSvcPushing, wfSvcHelm, wfSvcRollback, wfSvcPostSync:
		elapsed := DimStyle.Render(formatElapsed(time.Since(m.opStart)))
		sb.WriteString(fmt.Sprintf("  %s  %s\n", m.spinnerFrame(), elapsed))
	case wfServiceSelect:
		sb.WriteString(m.multisel.View().Content)
	case wfSvcTagSync:
		sb.WriteString(m.confirm.View().Content)
	case wfSvcDockerfile, wfSvcHelmError:
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
		case wfSvcTagSync, wfSvcDockerfile, wfSvcTagInput, wfSvcBuildArg:
			return stInteractive
		}
		return stPending
	}

	deployStatus := func() stStatus {
		if m.state >= wfSvcPostSync {
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
		{"Sync", asyncStatus(wfSvcPostSync, wfSummary)},
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

func wfRunPostDeploySync(cfg *config.Config, svc config.ServiceConfig, serviceName, newTag, valuesPath string) []string {
	var lines []string
	helmRoot := config.GetHelmRootPath()
	dockerRoot := config.GetDockerRootPath()
	commitMsg := logic.DeployCommitMessage(serviceName, newTag)

	if helmRoot != "" && svc.HelmValuesPath != "" && svc.HelmSetKey != "" {
		if err := logic.UpdateHelmValuesTag(valuesPath, svc.HelmSetKey, newTag, svc.HelmImagePath); err != nil {
			lines = append(lines, WarnStyle.Render("  ⚠  helm values non aggiornato: "+err.Error()))
		} else {
			lines = append(lines, SuccessStyle.Render("  ✓  Helm values aggiornato ("+svc.HelmValuesPath+")"))
			if err := logic.GitAdd(helmRoot, svc.HelmValuesPath); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git add helm: "+err.Error()))
			} else if err := logic.GitCommit(helmRoot, commitMsg); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git commit helm: "+err.Error()))
			} else if err := logic.GitPush(helmRoot); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git push helm: "+err.Error()))
			} else {
				lines = append(lines, SuccessStyle.Render("  ✓  Helm repo: commit e push completati."))
			}
		}
	}

	if dockerRoot != "" && svc.K8sManifestPath != "" && svc.K8sImageRef != "" {
		manifestAbs := filepath.Join(dockerRoot, svc.K8sManifestPath)
		if err := logic.UpdateK8sManifestImage(manifestAbs, svc.K8sImageRef, newTag); err != nil {
			lines = append(lines, WarnStyle.Render("  ⚠  k8s manifest non aggiornato: "+err.Error()))
		} else {
			lines = append(lines, SuccessStyle.Render("  ✓  K8s manifest aggiornato ("+svc.K8sManifestPath+")"))
			if err := logic.GitAdd(dockerRoot, svc.K8sManifestPath); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git add docker: "+err.Error()))
			} else if err := logic.GitCommit(dockerRoot, commitMsg); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git commit docker: "+err.Error()))
			} else if err := logic.GitPush(dockerRoot); err != nil {
				lines = append(lines, WarnStyle.Render("  ⚠  git push docker: "+err.Error()))
			} else {
				lines = append(lines, SuccessStyle.Render("  ✓  Docker repo: commit e push completati."))
			}
		}
	}

	return lines
}
