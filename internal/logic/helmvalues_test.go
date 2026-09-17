package logic

import (
	"os"
	"path/filepath"
	"testing"
)

// Shaped like a site values file: named image maps for the services,
// plus initContainer which carries a whole reference in a single string.
const sampleValues = `namespace: app-coll

initContainer:
  image: exampleacr.azurecr.io/apps/initc:dev

database:
  host: db.postgres.database.azure.com
  port: 5432

hostAliases:
  - ip: "10.0.0.10"
    hostnames:
      - "app.example.internal"

jbossAmq:
  name: jboss-amq
  replicas: 1
  image:
    repository: exampleacr.azurecr.io/apps/jboss-amq
    tag: 1.0.0-dev

jbossBe:
  name: jboss-be
  image:
    repository: exampleacr.azurecr.io/apps/jboss-be
    tag: 3.0.9-dev

webapp:
  image:
    repository: exampleacr.azurecr.io/apps/webapp
    tag: 3.0.9-dev

ingress:
  enabled: true
  className: nginx
`

func writeValues(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadHelmValues(t *testing.T) {
	got, err := ReadHelmValues(writeValues(t, sampleValues))
	if err != nil {
		t.Fatalf("lettura: %v", err)
	}

	if got.Namespace != "app-coll" {
		t.Errorf("namespace = %q", got.Namespace)
	}

	// database, hostAliases and ingress carry no image: they are configuration,
	// not deployable services.
	want := []string{"initc", "jboss-amq", "jboss-be", "webapp"}
	if len(got.Services) != len(want) {
		var names []string
		for _, s := range got.Services {
			names = append(names, s.Name)
		}
		t.Fatalf("servizi = %v, attesi %v", names, want)
	}
	for i, w := range want {
		if got.Services[i].Name != w {
			t.Errorf("servizio[%d] = %q, atteso %q", i, got.Services[i].Name, w)
		}
	}
}

// The Dockerfile is looked up by image name, so it must come from the repository
// path: the values key is camelCase and would need guesswork to convert.
func TestHelmServiceNameComesFromTheRepository(t *testing.T) {
	got, err := ReadHelmValues(writeValues(t, sampleValues))
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]HelmService{}
	for _, s := range got.Services {
		byName[s.Name] = s
	}

	be := byName["jboss-be"]
	if be.Name != "jboss-be" {
		t.Errorf("nome = %q, atteso jboss-be (da .../apps/jboss-be)", be.Name)
	}
	if be.Tag != "3.0.9-dev" {
		t.Errorf("tag = %q", be.Tag)
	}
	if len(be.Keys) != 1 || be.Keys[0].SetKey != "jbossBe.image.tag" {
		t.Errorf("chiavi = %+v", be.Keys)
	}
	if be.Keys[0].ImagePath != "" {
		t.Errorf("la forma con tag separato non deve avere ImagePath: %q", be.Keys[0].ImagePath)
	}
}

// initContainer holds "repository:tag" in one scalar: the update has to write
// the whole reference back, which is what ImagePath signals.
func TestHelmServiceWithInlineReference(t *testing.T) {
	got, err := ReadHelmValues(writeValues(t, sampleValues))
	if err != nil {
		t.Fatal(err)
	}

	var initc HelmService
	for _, s := range got.Services {
		if s.Name == "initc" {
			initc = s
		}
	}

	if initc.Tag != "dev" {
		t.Errorf("tag = %q, atteso dev", initc.Tag)
	}
	if len(initc.Keys) != 1 || initc.Keys[0].SetKey != "initContainer.image" {
		t.Errorf("chiavi = %+v", initc.Keys)
	}
	if initc.Keys[0].ImagePath != "exampleacr.azurecr.io/apps/initc" {
		t.Errorf("ImagePath = %q", initc.Keys[0].ImagePath)
	}
}

// A values file can reference .../apps/initc twice, in both shapes: one service
// with two keys, so a new tag lands on both instead of leaving one behind.
func TestSameImageReferencedTwice(t *testing.T) {
	const values = `namespace: app-coll

initContainer:
  image: acr.azurecr.io/apps/initc:dev

busybox:
  name: cbox
  image:
    repository: acr.azurecr.io/apps/initc
    tag: dev
`
	got, err := ReadHelmValues(writeValues(t, values))
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Services) != 1 {
		t.Fatalf("servizi = %d, atteso 1: initc è la stessa immagine", len(got.Services))
	}
	svc := got.Services[0]
	if svc.Name != "initc" {
		t.Errorf("nome = %q", svc.Name)
	}
	if len(svc.Keys) != 2 {
		t.Fatalf("chiavi = %d, attese 2 (initContainer e busybox)", len(svc.Keys))
	}
	if !svc.TagsAgree() {
		t.Error("i due riferimenti dichiarano lo stesso tag: TagsAgree dovrebbe essere true")
	}

	// Both references must be rewritten, each in its own shape.
	path := writeValues(t, values)
	for _, k := range svc.Keys {
		if err := UpdateHelmValuesTag(path, k.SetKey, "2.0.0", k.ImagePath); err != nil {
			t.Fatalf("aggiornamento di %s: %v", k.SetKey, err)
		}
	}
	after, err := ReadHelmValues(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Services[0].Tag != "2.0.0" || !after.Services[0].TagsAgree() {
		t.Errorf("dopo l'aggiornamento: tag=%q concordi=%v", after.Services[0].Tag, after.Services[0].TagsAgree())
	}
}

// Divergent tags mean the values are already inconsistent: say so, do not pick one.
func TestMixedTagsAreReported(t *testing.T) {
	const values = `initContainer:
  image: acr.azurecr.io/apps/initc:dev

busybox:
  image:
    repository: acr.azurecr.io/apps/initc
    tag: 1.0.0
`
	got, err := ReadHelmValues(writeValues(t, values))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 {
		t.Fatalf("servizi = %d, atteso 1", len(got.Services))
	}
	if got.Services[0].TagsAgree() {
		t.Error("tag divergenti: TagsAgree dovrebbe essere false")
	}
}

// The values read here are written back by the sync: the round trip must land
// on the same key it came from.
func TestHelmServiceRoundTrip(t *testing.T) {
	path := writeValues(t, sampleValues)

	before, err := ReadHelmValues(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range before.Services {
		for _, k := range s.Keys {
			if err := UpdateHelmValuesTag(path, k.SetKey, "9.9.9-test", k.ImagePath); err != nil {
				t.Fatalf("aggiornamento di %s: %v", k.SetKey, err)
			}
		}
	}

	after, err := ReadHelmValues(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Services) != len(before.Services) {
		t.Fatalf("servizi persi nel round trip: %d → %d", len(before.Services), len(after.Services))
	}
	for i, s := range after.Services {
		if s.Tag != "9.9.9-test" {
			t.Errorf("%s: tag = %q dopo l'aggiornamento", s.Name, s.Tag)
		}
		if s.Repository != before.Services[i].Repository {
			t.Errorf("%s: repository alterato: %q → %q", s.Name, before.Services[i].Repository, s.Repository)
		}
	}
	if after.Namespace != "app-coll" {
		t.Errorf("namespace alterato: %q", after.Namespace)
	}
}

func TestReadChartVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"),
		[]byte("apiVersion: v2\nname: app\nversion: 0.7.0\nappVersion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadChartVersion(dir)
	if err != nil {
		t.Fatalf("lettura: %v", err)
	}
	if got != "0.7.0" {
		t.Errorf("version = %q, attesa 0.7.0", got)
	}

	if _, err := ReadChartVersion(t.TempDir()); err == nil {
		t.Error("Chart.yaml assente: atteso errore")
	}
}

// The Deployment name comes from `name` inside the block, not from the image and
// not from the values key. On a typical chart the three coincide, which is
// exactly why a restart that guessed would look right until it did not.
const namedValues = `namespace: app-coll

initContainer:
  image: acr.azurecr.io/apps/initc:dev

railBe:
  name: rail-be-api
  image:
    repository: acr.azurecr.io/apps/rail-be
    tag: 1.0.0

webapp:
  image:
    repository: acr.azurecr.io/apps/webapp
    tag: 3.0.9-dev
`

func TestReadHelmValuesTakesDeploymentFromName(t *testing.T) {
	values, err := ReadHelmValues(writeValues(t, namedValues))
	if err != nil {
		t.Fatalf("lettura: %v", err)
	}

	cases := map[string]string{
		"railBe.image.tag":    "rail-be-api",
		"webapp.image.tag":    "",
		"initContainer.image": "",
	}
	for setKey, want := range cases {
		if got := values.DeploymentFor(setKey); got != want {
			t.Errorf("DeploymentFor(%q) = %q, atteso %q", setKey, got, want)
		}
	}
	if got := values.DeploymentFor("assente.image.tag"); got != "" {
		t.Errorf("chiave assente: %q, atteso vuoto", got)
	}
}

// An image referenced twice runs in two Deployments, and a reference outside a
// Deployment has no name at all: both have to come back, because one cannot be
// restarted and the caller has to say so instead of dropping it.
func TestServiceDeploymentsSplitNamedFromUnnamed(t *testing.T) {
	svc := HelmService{Keys: []HelmImageKey{
		{Key: "jbossBe", Deployment: "jboss-be"},
		{Key: "jbossBeWorker", Deployment: "jboss-be"},
		{Key: "jbossFe", Deployment: "jboss-fe"},
		{Key: "initContainer"},
	}}

	names, unnamed := svc.Deployments()
	if len(names) != 2 || names[0] != "jboss-be" || names[1] != "jboss-fe" {
		t.Errorf("deployment = %v, attesi [jboss-be jboss-fe]", names)
	}
	if len(unnamed) != 1 || unnamed[0] != "initContainer" {
		t.Errorf("senza nome = %v, atteso [initContainer]", unnamed)
	}
}

// What `helm get values -o json` returns for a release installed from
// sampleValues, after an upgrade that bumped jboss-be and initc with --set but
// never wrote them back. jbossAmq is absent: a key the release does not carry
// keeps the tag of the values file.
func deployedSample() map[string]any {
	return map[string]any{
		"namespace":     "app-coll",
		"initContainer": map[string]any{"image": "exampleacr.azurecr.io/apps/initc:1.0.1"},
		"jbossBe": map[string]any{
			"name":  "jboss-be",
			"image": map[string]any{"repository": "exampleacr.azurecr.io/apps/jboss-be", "tag": "3.0.11-dev"},
		},
		"webapp": map[string]any{
			"image": map[string]any{"repository": "exampleacr.azurecr.io/apps/webapp", "tag": "3.0.9-dev"},
		},
	}
}

// The deployed release wins: the current tag is the one running, and every
// reference that disagreed is reported with both tags.
func TestAlignToDeployed(t *testing.T) {
	values, err := ReadHelmValues(writeValues(t, sampleValues))
	if err != nil {
		t.Fatal(err)
	}

	drift := values.AlignToDeployed(deployedSample())

	got := map[string]TagDrift{}
	for _, d := range drift {
		got[d.Service] = d
	}
	if len(drift) != 2 {
		t.Fatalf("scostamenti = %+v, attesi 2 (initc e jboss-be)", drift)
	}
	if d := got["jboss-be"]; d.Values != "3.0.9-dev" || d.Deployed != "3.0.11-dev" || d.Key.SetKey != "jbossBe.image.tag" {
		t.Errorf("jboss-be: %+v", d)
	}
	if d := got["initc"]; d.Values != "dev" || d.Deployed != "1.0.1" || d.Key.ImagePath == "" {
		t.Errorf("initc: la forma inline deve portare il solo tag e il path: %+v", d)
	}

	tags := map[string]string{}
	for _, s := range values.Services {
		tags[s.Name] = s.Tag
	}
	want := map[string]string{"initc": "1.0.1", "jboss-amq": "1.0.0-dev", "jboss-be": "3.0.11-dev", "webapp": "3.0.9-dev"}
	for name, tag := range want {
		if tags[name] != tag {
			t.Errorf("%s: tag corrente %q, atteso %q", name, tags[name], tag)
		}
	}
}

// Nothing deployed, nothing to align: a release installed for the first time
// starts from the values file.
func TestAlignToDeployedWithoutRelease(t *testing.T) {
	values, err := ReadHelmValues(writeValues(t, sampleValues))
	if err != nil {
		t.Fatal(err)
	}
	if drift := values.AlignToDeployed(nil); len(drift) != 0 {
		t.Errorf("scostamenti = %+v, attesi nessuno", drift)
	}
	if values.Services[2].Tag != "3.0.9-dev" {
		t.Errorf("tag alterato: %q", values.Services[2].Tag)
	}
}

// Two references of one image are compared one by one: the one left behind is
// the one reported, and the service now disagrees with itself.
func TestAlignToDeployedPerReference(t *testing.T) {
	const values = `initContainer:
  image: acr.azurecr.io/apps/initc:dev

busybox:
  name: cbox
  image:
    repository: acr.azurecr.io/apps/initc
    tag: dev
`
	got, err := ReadHelmValues(writeValues(t, values))
	if err != nil {
		t.Fatal(err)
	}
	drift := got.AlignToDeployed(map[string]any{
		"initContainer": map[string]any{"image": "acr.azurecr.io/apps/initc:dev"},
		"busybox":       map[string]any{"image": map[string]any{"tag": "1.0.1"}},
	})
	if len(drift) != 1 || drift[0].Key.Key != "busybox" {
		t.Fatalf("scostamenti = %+v, atteso solo busybox", drift)
	}
	if got.Services[0].TagsAgree() {
		t.Error("dopo l'allineamento i due riferimenti divergono: TagsAgree dovrebbe essere false")
	}
}
