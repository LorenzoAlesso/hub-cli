package ui

import (
	"fmt"
	"slices"
	"strings"
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
// the two tags side by side, then what the run does about it.
func logDrift(drift []logic.TagDrift, elapsed string) []string {
	lines := []string{logWarnStep(psnReleaseLabel, "values non allineato", elapsed)}

	width := 0
	for _, d := range drift {
		width = max(width, lipgloss.Width(d.Service))
	}

	// Two references of one image that lag the same way read as one fact.
	var seen []string
	for _, d := range drift {
		pair := d.Service + " " + d.Values + " " + d.Deployed
		if slices.Contains(seen, pair) {
			continue
		}
		seen = append(seen, pair)
		lines = append(lines, "     "+ValueStyle.Render(d.Service)+
			pad(lipgloss.Width(d.Service), width+2)+
			DimStyle.Render("values ")+WarnStyle.Render(d.Values)+
			DimStyle.Render("  ·  deployato ")+ValueStyle.Render(d.Deployed))
	}
	return append(lines, logDetail("Si parte dai tag deployati, e il sync li riporta nel values."))
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

// psnKeptNote lists the tags the upgrade kept as the cluster runs them.
func psnKeptNote(drift []logic.TagDrift) string {
	parts := make([]string, 0, len(drift))
	for _, s := range driftServices(drift) {
		parts = append(parts, s.Name+" "+s.Tag)
	}
	return strings.Join(parts, ", ")
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

// psnRepoCount is the note of the ACR step.
func psnRepoCount(n int) string {
	return fmt.Sprintf("%d repository", n)
}

// logACRNote explains a proposal that skips ahead: ACR already holds an image
// newer than the one running, and proposing its tag again would overwrite it.
func logACRNote(highest, suggested string) string {
	return logInfo("Su ACR", "presente fino a "+highest+"  ·  proposto "+suggested)
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
