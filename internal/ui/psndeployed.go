package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"Hub-cli/internal/logic"

	"charm.land/lipgloss/v2"
)

// The values file says what was last written back, not what runs: a deploy
// that skipped the sync, or one made by hand, leaves it behind. The run starts
// from the deployed release instead, keeps what it runs through the upgrade and
// writes it back with the sync, so the branch ends up describing the cluster.

// psnReleaseLabel names the step that reads the deployed release.
const psnReleaseLabel = "Release sul cluster"

// A failed read of the release almost always means the cluster cannot be
// reached — a VPN or DNS hiccup on a private cluster — and then the upgrade at
// the end would fail the same way, after minutes of builds. The read is tried
// again once on its own; past that, the run asks instead of going on blind.
const (
	releaseReadAttempts   = 2
	releaseReadRetryDelay = 3 * time.Second
)

// releaseReadNote names a failed read in the note column.
func releaseReadNote(err error) string {
	if clusterUnreachable(err) {
		return "cluster non raggiungibile"
	}
	return "tag deployati non leggibili"
}

// releaseReadDetail keeps what explains an unreachable cluster — the lookup or
// the dial that failed — out of the chain Helm wraps it in. Any other error is
// shown whole: there is no telling which part matters.
func releaseReadDetail(err error) string {
	msg := err.Error()
	if !clusterUnreachable(err) {
		return msg
	}
	rest := msg
	// Helm versions differ on the capital.
	for _, marker := range []string{"Kubernetes cluster unreachable: ", "kubernetes cluster unreachable: "} {
		if _, after, ok := strings.Cut(msg, marker); ok {
			rest = after
			break
		}
	}
	if idx := strings.Index(rest, "dial tcp"); idx != -1 {
		rest = strings.TrimPrefix(rest[idx:], "dial tcp: ")
	}
	return strings.TrimSpace(rest)
}

// logReleaseRetry is a read that failed and is about to be repeated.
func logReleaseRetry(attempt int, err error, elapsed string) []string {
	detail := releaseReadDetail(err)
	if clusterUnreachable(err) {
		detail = releaseReadNote(err) + ": " + detail
	}
	return []string{
		logWarnStep(psnReleaseLabel, fmt.Sprintf("tentativo %d/%d non riuscito", attempt, releaseReadAttempts), elapsed),
		logDetail(detail),
	}
}

// logReleaseFailure is the last attempt failed, with the reason underneath.
func logReleaseFailure(err error, elapsed string) []string {
	return []string{logFail(psnReleaseLabel, releaseReadNote(err), elapsed), logDetail(releaseReadDetail(err))}
}

// releaseErrorTitle and releaseErrorItems are the question asked once every
// attempt has failed. Retrying comes first: a reconnected VPN is the usual fix.
func releaseErrorTitle(err error) string {
	if clusterUnreachable(err) {
		return "Il cluster non risponde"
	}
	return "Release non leggibile"
}

func releaseErrorItems(err error) []Item {
	retry := "rilegge il release"
	if clusterUnreachable(err) {
		retry += " (VPN o DNS ripristinati)"
	}
	return []Item{
		{Value: "retry", Label: "Riprova", Desc: retry},
		{Value: "continue", Label: "Prosegui", Desc: "parte dai tag del values: l'upgrade finale potrebbe fallire"},
		{Value: "cancel", Label: "Annulla", Desc: "esce prima di buildare"},
	}
}

func clusterUnreachable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "cluster unreachable")
}

// In --test-ui the first read of the release fails the way a private cluster
// does when the VPN drops for a moment, and the second one goes through: the
// retry would otherwise be a path no simulated run ever shows. The host is
// under a reserved domain.
var errSimulatedUnreachable = errors.New(`helm get values fallito: Error: Kubernetes cluster unreachable: ` +
	`Get "https://aks-demo.privatelink.example.com:443/version": dial tcp: lookup aks-demo.privatelink.example.com: no such host`)

// logDrift reports the images whose values tag is not the one the release runs,
// one line each. What the run does about it is not explained here: it shows
// where it happens, in the starting tag of each service and in the release.
func logDrift(drift []logic.TagDrift, elapsed string) []string {
	// Two references of one image that lag the same way read as one fact.
	var pairs []logic.TagDrift
	for _, d := range drift {
		same := func(p logic.TagDrift) bool {
			return p.Service == d.Service && p.Values == d.Values && p.Deployed == d.Deployed
		}
		if !slices.ContainsFunc(pairs, same) {
			pairs = append(pairs, d)
		}
	}

	note := "1 tag diverso dal values"
	if len(pairs) != 1 {
		note = fmt.Sprintf("%d tag diversi dal values", len(pairs))
	}
	lines := []string{logWarnStep(psnReleaseLabel, note, elapsed)}

	width := 0
	for _, d := range pairs {
		width = max(width, lipgloss.Width(d.Service))
	}
	for _, d := range pairs {
		lines = append(lines, "     "+ValueStyle.Render(d.Service)+
			pad(lipgloss.Width(d.Service), width+2)+
			WarnStyle.Render(d.Values)+DimStyle.Render(" nel values, ")+
			ValueStyle.Render(d.Deployed)+DimStyle.Render(" sul cluster"))
	}
	return lines
}

// driftServices is one entry per image and tag realigned, for the commit message.
func driftServices(drift []logic.TagDrift) []logic.DeployedService {
	var out []logic.DeployedService
	for _, d := range drift {
		svc := logic.DeployedService{Name: d.Service, Tag: d.Deployed}
		if !slices.Contains(out, svc) {
			out = append(out, svc)
		}
	}
	return out
}

// helmSetArg is the --set that gives one values reference its tag, in the shape
// the reference has.
func helmSetArg(k logic.HelmImageKey, tag string) string {
	if k.ImagePath != "" {
		return fmt.Sprintf("%s=%s:%s", k.SetKey, k.ImagePath, tag)
	}
	return fmt.Sprintf("%s=%s", k.SetKey, tag)
}

// logKept says, under the upgrade, which images kept the tag the cluster runs.
func logKept(drift []logic.TagDrift) []string {
	var lines []string
	for _, s := range driftServices(drift) {
		lines = append(lines, logDetail(s.Name+" resta a "+s.Tag+", come sul cluster"))
	}
	return lines
}

// psnRealignedNote counts the tags the sync wrote back without deploying them.
func psnRealignedNote(drift []logic.TagDrift) string {
	n := len(driftServices(drift))
	if n == 1 {
		return "1 tag riallineato"
	}
	return fmt.Sprintf("%d tag riallineati", n)
}

// fetchACRTags reads the tags of every repository at once: az takes seconds per
// call, and one after the other they would add up to a wait worth noticing.
func fetchACRTags(repos []string) psnTagsLoadedMsg {
	msg := psnTagsLoadedMsg{tags: map[string][]string{}, errs: map[string]error{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Go(func() {
			tags, err := logic.ACRTags(repo)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				msg.errs[repo] = err
				return
			}
			msg.tags[repo] = tags
		})
	}
	wg.Wait()
	return msg
}

// takenTagLines lists, in the question about them, the tags already on ACR.
func takenTagLines(names, tags []string) []string {
	width := 0
	for _, n := range names {
		width = max(width, lipgloss.Width(n))
	}
	lines := make([]string, len(names))
	for i, n := range names {
		lines[i] = ValueStyle.Render(n) + pad(lipgloss.Width(n), width+3) + WarnStyle.Render(tags[i])
	}
	return lines
}

func overwriteDesc(count int) string {
	if count == 1 {
		return "il push sostituisce l'immagine esistente"
	}
	return "il push sostituisce le immagini esistenti"
}

// In --test-ui the release runs jboss-fe one tag ahead of the values, the way a
// deploy that skipped the sync leaves it, and ACR already holds a webapp newer
// than the one running: without them the alignment and the skipped proposal
// would be paths no simulated run ever shows.
func psnFakeDeployed() map[string]any {
	return map[string]any{
		"webapp":  map[string]any{"image": map[string]any{"tag": "1.0.0"}},
		"jbossFe": map[string]any{"image": map[string]any{"tag": "2.1.4"}},
	}
}

func psnFakeACRTags(repos []string) map[string][]string {
	fake := map[string][]string{
		"demoacr.azurecr.io/demo/webapp":   {"1.0.0", "1.0.1"},
		"demoacr.azurecr.io/demo/jboss-fe": {"2.1.3", "2.1.4"},
	}
	out := make(map[string][]string, len(repos))
	for _, repo := range repos {
		out[repo] = fake[repo]
	}
	return out
}
