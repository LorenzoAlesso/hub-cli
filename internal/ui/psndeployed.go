package ui

import (
	"fmt"
	"slices"
	"sync"

	"Hub-cli/internal/logic"

	"charm.land/lipgloss/v2"
)

// The values file says what was last written back, not what runs: a deploy
// that skipped the sync, or one made by hand, leaves it behind. The run starts
// from the deployed release instead, keeps what it runs through the upgrade and
// writes it back with the sync, so the branch ends up describing the cluster.

// psnReleaseLabel names the step that reads the deployed release.
const psnReleaseLabel = "Release sul cluster"

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

// logACRNote explains a proposal that skips ahead: ACR already holds an image
// newer than the one running. The proposal itself is in the tag field.
func logACRNote(highest string) string {
	return logInfo("Su ACR", "tag immagine "+highest+" già presente")
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
