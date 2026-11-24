//go:build docs

package docs_test

import (
	"maps"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The published images are what an operator deploys without building, and
// nothing in this repository pulls them back down: the workflow runs only when
// somebody publishes a release. These cases hold what decides whether a release
// produces images that can be trusted and run — which images, under which tags,
// with which token, and the one compose file that consumes them.

const (
	imageWorkflow  = ".github/workflows/images.yml"
	releaseCompose = "deploy/compose/docker-compose.yml"
	gatewayEnv     = "deploy/compose/gateway.env.example"
	migrateEnv     = "deploy/compose/migrate.env.example"
	edgeConf       = "deploy/compose/edge.conf"
	deployRunbook  = "docs/runbooks/deploy-from-published-images.md"
	onRelease      = "github.event_name == 'release'"
	imageOwner     = "ghcr.io/enchang1017/"
)

var productionImages = map[string]string{
	"avior":          "deploy/standalone/Dockerfile",
	"avior-backend":  "backend/Dockerfile",
	"avior-frontend": "frontend/Dockerfile",
	"avior-migrate":  "deploy/migrate/Dockerfile",
}

// Built from source by docker-compose.yml for the local stack and never shipped.
var developmentDockerfiles = []string{
	"backend/Dockerfile.dev",
	"frontend/Dockerfile.dev",
	"deploy/seed/Dockerfile",
}

type workflowFile struct {
	On          map[string]any         `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	If          string            `yaml:"if"`
	Permissions map[string]string `yaml:"permissions"`
	Strategy    struct {
		Matrix struct {
			Include []map[string]string `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	ID   string            `yaml:"id"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

func (j workflowJob) step(action string) (workflowStep, bool) {
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, action+"@") {
			return s, true
		}
	}
	return workflowStep{}, false
}

func (j workflowJob) matrixImages() map[string]string {
	got := map[string]string{}
	for _, entry := range j.Strategy.Matrix.Include {
		got[entry["image"]] = entry["dockerfile"]
	}
	return got
}

func loadWorkflow(t *testing.T) workflowFile {
	t.Helper()
	var wf workflowFile
	if err := yaml.Unmarshal([]byte(readRepoFile(t, imageWorkflow)), &wf); err != nil {
		t.Fatalf("parse %s: %v", imageWorkflow, err)
	}
	return wf
}

// publishJob is the one job allowed to write to the registry, and buildJobs are
// every other one. A second writer would be a second way to move `latest`.
func splitJobs(t *testing.T, wf workflowFile) (workflowJob, map[string]workflowJob) {
	t.Helper()
	var publish []string
	builds := map[string]workflowJob{}
	for name, job := range wf.Jobs {
		if job.Permissions["packages"] == "write" {
			publish = append(publish, name)
			continue
		}
		builds[name] = job
	}
	if len(publish) != 1 {
		t.Fatalf("%s has %d jobs granted packages: write (%v), want exactly one", imageWorkflow, len(publish), publish)
	}
	if len(builds) == 0 {
		t.Fatalf("%s has no build-only job; a pull request would not build the images before a release does", imageWorkflow)
	}
	return wf.Jobs[publish[0]], builds
}

func TestEveryProductionDockerfileIsPublished(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = repoRoot
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var found []string
	for _, path := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(filepath.Base(path), "Dockerfile") {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Fatal("git ls-files lists no Dockerfile; this gate reads tracked files")
	}
	for _, path := range found {
		if slices.Contains(developmentDockerfiles, path) {
			continue
		}
		published := false
		for _, dockerfile := range productionImages {
			published = published || dockerfile == path
		}
		if !published {
			t.Errorf("%s is neither a published image nor a development one; add it to productionImages and %s", path, imageWorkflow)
		}
	}

	wf := loadWorkflow(t)
	publish, builds := splitJobs(t, wf)
	for name, job := range builds {
		if got := job.matrixImages(); !maps.Equal(got, productionImages) {
			t.Errorf("job %s builds %v, want %v", name, got, productionImages)
		}
	}
	if got := publish.matrixImages(); !maps.Equal(got, productionImages) {
		t.Errorf("the publish job pushes %v, want %v", got, productionImages)
	}
}

func TestImagesArePublishedOnlyWhenAReleaseIsPublished(t *testing.T) {
	wf := loadWorkflow(t)

	release, ok := wf.On["release"].(map[string]any)
	if !ok {
		t.Fatalf("%s does not run on release", imageWorkflow)
	}
	if types, _ := release["types"].([]any); len(types) != 1 || types[0] != "published" {
		t.Errorf("%s runs on release types %v, want exactly [published]", imageWorkflow, release["types"])
	}
	for _, want := range []string{"pull_request", "workflow_dispatch"} {
		if _, ok := wf.On[want]; !ok {
			t.Errorf("%s does not run on %s; the images would first be built by a release", imageWorkflow, want)
		}
	}
	for _, refused := range []string{"pull_request_target", "push", "schedule"} {
		if _, ok := wf.On[refused]; ok {
			t.Errorf("%s runs on %s; publishing belongs to a release alone", imageWorkflow, refused)
		}
	}

	publish, builds := splitJobs(t, wf)
	if publish.If != onRelease {
		t.Errorf("the publish job runs if %q, want %q", publish.If, onRelease)
	}
	for name, job := range builds {
		build, ok := job.step("docker/build-push-action")
		if !ok {
			t.Errorf("job %s does not build the images", name)
			continue
		}
		if build.With["push"] != "false" {
			t.Errorf("job %s pushes (push: %q); only the release job may", name, build.With["push"])
		}
		if _, ok := job.step("docker/login-action"); ok {
			t.Errorf("job %s logs in to a registry; only the release job may", name)
		}
	}
}

func TestImageWorkflowHoldsTheLeastItNeeds(t *testing.T) {
	wf := loadWorkflow(t)
	if !maps.Equal(wf.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("%s grants %v by default, want only contents: read", imageWorkflow, wf.Permissions)
	}
	publish, builds := splitJobs(t, wf)
	if !maps.Equal(publish.Permissions, map[string]string{"contents": "read", "packages": "write"}) {
		t.Errorf("the publish job grants %v, want contents: read and packages: write", publish.Permissions)
	}
	for name, job := range builds {
		for scope, level := range job.Permissions {
			if level != "read" {
				t.Errorf("job %s grants %s: %s", name, scope, level)
			}
		}
	}

	raw := readRepoFile(t, imageWorkflow)
	for _, m := range regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`).FindAllStringSubmatch(raw, -1) {
		if m[1] != "GITHUB_TOKEN" {
			t.Errorf("%s reads secrets.%s; the registry is reached with the job's own token", imageWorkflow, m[1])
		}
	}
	for name, job := range wf.Jobs {
		for _, s := range job.Steps {
			if strings.Contains(s.Run, "${{") {
				t.Errorf("job %s interpolates an expression into a shell script; pass it through env: %q", name, s.Run)
			}
			if strings.HasPrefix(s.Uses, "actions/checkout@") && s.With["persist-credentials"] != "false" {
				t.Errorf("job %s leaves the checkout's token in .git/config", name)
			}
		}
	}
}

const (
	releaseVersionOut = "${{ steps.version.outputs.version }}"
	releaseIsStable   = "steps.version.outputs.stable == 'true' && !github.event.release.prerelease"
)

// stepIndex is the position of the first step match accepts,
// and -1 when there is none.
func (j workflowJob) stepIndex(match func(workflowStep) bool) int {
	return slices.IndexFunc(j.Steps, match)
}

func TestAReleaseIsTaggedWithItsVersionAndLatest(t *testing.T) {
	publish, _ := splitJobs(t, loadWorkflow(t))

	version := publish.stepIndex(func(s workflowStep) bool {
		return s.ID == "version" && s.Env["TAG"] == "${{ github.event.release.tag_name }}" &&
			strings.TrimSpace(s.Run) == `.github/scripts/release-version.sh "$TAG"`
	})
	meta := publish.stepIndex(func(s workflowStep) bool { return strings.HasPrefix(s.Uses, "docker/metadata-action@") })
	check := publish.stepIndex(func(s workflowStep) bool {
		return strings.TrimSpace(s.Run) == ".github/scripts/check-release-tags.sh" &&
			s.Env["IMAGE"] == "ghcr.io/${{ github.repository_owner }}/${{ matrix.image }}" &&
			s.Env["VERSION"] == releaseVersionOut &&
			s.Env["STABLE"] == "${{ "+releaseIsStable+" }}" &&
			s.Env["TAGS"] == "${{ steps.meta.outputs.tags }}"
	})
	push := publish.stepIndex(func(s workflowStep) bool { return strings.HasPrefix(s.Uses, "docker/build-push-action@") })
	if version < 0 || meta < 0 || check < 0 || push < 0 || !(version < meta && meta < check && check < push) {
		t.Fatalf("the publish job must read the version from the tag, derive the tags, check them, then push; "+
			"found those steps at %d, %d, %d and %d", version, meta, check, push)
	}

	m := publish.Steps[meta]
	if m.With["images"] != "ghcr.io/${{ github.repository_owner }}/${{ matrix.image }}" || m.With["flavor"] != "latest=false" {
		t.Errorf("the publish job names its images %q with flavor %q", m.With["images"], m.With["flavor"])
	}
	tags := strings.Split(strings.TrimSpace(m.With["tags"]), "\n")
	want := []string{
		"type=raw,value=" + releaseVersionOut,
		"type=raw,value=latest,enable=${{ " + releaseIsStable + " }}",
	}
	if !slices.Equal(tags, want) {
		t.Errorf("the publish job's tags are %q, want exactly %q", tags, want)
	}
	if b := publish.Steps[push]; b.With["push"] != "true" || b.With["build-args"] != "VERSION="+releaseVersionOut {
		t.Errorf("the publish job pushes %q with build-args %q, want true and the checked version", b.With["push"], b.With["build-args"])
	}

	login, ok := publish.step("docker/login-action")
	if !ok || login.With["registry"] != "ghcr.io" || login.With["password"] != "${{ secrets.GITHUB_TOKEN }}" {
		t.Errorf("the publish job does not log in to ghcr.io with the job's token: %v", login.With)
	}
	readBack := publish.stepIndex(func(s workflowStep) bool {
		return strings.Contains(s.Run, "imagetools inspect") && s.Env["TAGS"] == "${{ steps.meta.outputs.tags }}"
	})
	if readBack < push {
		t.Error("the publish job does not read every pushed tag back from the registry after pushing")
	}
}

// A pull request builds exactly what a release would push, so the first build
// of an image for arm64 is never the one that publishes it.
func TestEveryJobBuildsBothPlatformsAndStampsTheVersion(t *testing.T) {
	publish, builds := splitJobs(t, loadWorkflow(t))
	all := maps.Clone(builds)
	all["publish"] = publish
	for name, job := range all {
		build, ok := job.step("docker/build-push-action")
		if !ok {
			t.Errorf("job %s does not build the images", name)
			continue
		}
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			if !strings.Contains(build.With["platforms"], platform) {
				t.Errorf("job %s does not build %s", name, platform)
			}
		}
		if build.With["file"] != "${{ matrix.dockerfile }}" || build.With["context"] != "." {
			t.Errorf("job %s builds from context %q and file %q, want . and the matrix's Dockerfile", name, build.With["context"], build.With["file"])
		}
		stamp := "VERSION=${{ steps.meta.outputs.version }}"
		if name == "publish" {
			stamp = "VERSION=" + releaseVersionOut
		}
		if build.With["build-args"] != stamp {
			t.Errorf("job %s stamps %q, want %q", name, build.With["build-args"], stamp)
		}
	}
}

type composeFile struct {
	Name     string                    `yaml:"name"`
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string                       `yaml:"image"`
	Build       any                          `yaml:"build"`
	EnvFile     any                          `yaml:"env_file"`
	Environment map[string]string            `yaml:"environment"`
	Ports       []string                     `yaml:"ports"`
	Volumes     []string                     `yaml:"volumes"`
	ReadOnly    bool                         `yaml:"read_only"`
	CapDrop     []string                     `yaml:"cap_drop"`
	DependsOn   map[string]map[string]string `yaml:"depends_on"`
}

func loadCompose(t *testing.T, name string) composeFile {
	t.Helper()
	var c composeFile
	if err := yaml.Unmarshal([]byte(readRepoFile(t, name)), &c); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return c
}

func TestReleaseComposeOnlyPullsPublishedImages(t *testing.T) {
	c := loadCompose(t, releaseCompose)
	image := regexp.MustCompile(`^ghcr\.io/\$\{AVIOR_IMAGE_OWNER:-enchang1017\}/(avior-[a-z]+):\$\{AVIOR_VERSION:\?[^}]+\}$`)

	used := map[string]string{}
	for name, svc := range c.Services {
		if svc.Build != nil {
			t.Errorf("%s builds service %s; this file deploys published images and nothing else", releaseCompose, name)
		}
		if m := image.FindStringSubmatch(svc.Image); m != nil {
			used[m[1]] = productionImages[m[1]]
		}
	}
	splitImages := maps.Clone(productionImages)
	delete(splitImages, "avior") // The standalone image replaces this compose stack.
	if !maps.Equal(used, splitImages) {
		t.Errorf("%s pulls %v at the release's version, want the split deployment images %v", releaseCompose, used, splitImages)
	}

	edge, ok := c.Services["edge"]
	if !ok {
		t.Fatalf("%s has no edge service to put the console and the gateway on one origin", releaseCompose)
	}
	console := regexp.MustCompile(`(?m)^FROM (nginx:\S+)\s*$`).FindStringSubmatch(readRepoFile(t, "frontend/Dockerfile"))
	if console == nil || edge.Image != console[1] {
		t.Errorf("the edge runs %q and the console image is built on %v; pin them together", edge.Image, console)
	}
	if !slices.Equal(edge.Ports, []string{"${AVIOR_PUBLISH:-127.0.0.1:8080}:8080"}) {
		t.Errorf("the edge publishes %v; it serves plain HTTP and belongs on loopback behind TLS unless told otherwise", edge.Ports)
	}
	if !slices.Contains(edge.Volumes, "./edge.conf:/etc/nginx/conf.d/default.conf:ro") {
		t.Errorf("the edge does not mount edge.conf read-only: %v", edge.Volumes)
	}
}

func TestReleaseComposeRunsTheGatewayAsProduction(t *testing.T) {
	c := loadCompose(t, releaseCompose)
	if c.Name == "" || c.Name == "avior" {
		t.Errorf("%s is project %q; the main checkout's development stack is avior", releaseCompose, c.Name)
	}
	gw, ok := c.Services["backend"]
	if !ok {
		t.Fatalf("%s has no backend service", releaseCompose)
	}
	if gw.Environment["AVIOR_ENV"] != "${AVIOR_ENV:-production}" {
		t.Errorf("the gateway's AVIOR_ENV is %q; unset must mean production, never the binary's development default", gw.Environment["AVIOR_ENV"])
	}
	if gw.EnvFile != "gateway.env" {
		t.Errorf("the gateway reads its environment from %v, want gateway.env", gw.EnvFile)
	}
	if gw.DependsOn["migrate"]["condition"] != "service_completed_successfully" {
		t.Error("the gateway does not wait for the migration to finish")
	}
	if !gw.ReadOnly || !slices.Equal(gw.CapDrop, []string{"ALL"}) {
		t.Error("the gateway holds every database credential and must run read-only with every capability dropped")
	}
	for name, svc := range c.Services {
		for _, v := range svc.Volumes {
			if strings.Contains(v, "/run/secrets") && !strings.HasSuffix(v, ":ro") {
				t.Errorf("service %s mounts its secrets writable: %s", name, v)
			}
		}
		for key, value := range svc.Environment {
			if isInlineSecret(key) {
				t.Errorf("service %s carries %s inline; mount it and name it with %s_FILE", name, key, key)
			}
			if value == "trusted-local" || (key == "AVIOR_SECURE_COOKIES" && value == "false") {
				t.Errorf("service %s sets %s=%s, which is development only", name, key, value)
			}
		}
	}
	if mig := c.Services["migrate"]; mig.Environment["AVIOR_STORE_PASSWORD_FILE"] == "" {
		t.Error("the migration reads no password file; its credential would be in the DSN")
	}
}

func TestEnvExamplesNameFilesNotSecrets(t *testing.T) {
	required := map[string][]string{
		gatewayEnv: {"AVIOR_STORE_DSN", "AVIOR_TARGET_DSN", "AVIOR_SESSION_SECRET_FILE", "AVIOR_AUTH_MODE"},
		migrateEnv: {"AVIOR_STORE_DSN"},
	}
	assignment := regexp.MustCompile(`^(AVIOR_[A-Z_]+)=(.*)$`)
	for file, want := range required {
		seen := map[string]bool{}
		for n, line := range strings.Split(readRepoFile(t, file), "\n") {
			m := assignment.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			key, value := m[1], m[2]
			seen[key] = true
			if isInlineSecret(key) {
				t.Errorf("%s:%d sets %s inline; name the file with %s_FILE", file, n+1, key, key)
			}
			if strings.HasSuffix(key, "_FILE") && !strings.HasPrefix(value, "/run/secrets/avior/") {
				t.Errorf("%s:%d points %s outside the mounted secrets: %s", file, n+1, key, value)
			}
			if strings.Contains(value, "trusted-local") || strings.Contains(value, "local-development") ||
				(key == "AVIOR_SECURE_COOKIES" && value == "false") {
				t.Errorf("%s:%d carries a development-only value: %s", file, n+1, line)
			}
			if strings.Contains(value, "sslmode=disable") || regexp.MustCompile(`://[^/@]+:[^/@]+@`).MatchString(value) {
				t.Errorf("%s:%d reaches the server in plaintext or with a password in the DSN: %s", file, n+1, line)
			}
		}
		for _, key := range want {
			if !seen[key] {
				t.Errorf("%s does not set %s, which its container refuses to start without", file, key)
			}
		}
	}

	ignore := readRepoFile(t, ".gitignore")
	for _, want := range []string{"/deploy/compose/gateway.env", "/deploy/compose/migrate.env", "/deploy/compose/secrets/"} {
		if !strings.Contains(ignore, want+"\n") {
			t.Errorf(".gitignore does not keep %s out of the repository", want)
		}
	}
}

func TestEdgeSendsTheApiToTheGatewayAndTheRestToTheConsole(t *testing.T) {
	conf := readRepoFile(t, edgeConf)
	for location, upstream := range map[string]string{
		"location /api/ {": "proxy_pass http://backend:8080;",
		"location / {":     "proxy_pass http://frontend:8080;",
	} {
		_, block, ok := strings.Cut(conf, location)
		if !ok {
			t.Errorf("%s has no %s", edgeConf, location)
			continue
		}
		block, _, _ = strings.Cut(block, "}")
		if !strings.Contains(block, upstream) {
			t.Errorf("%s %s does not %s", edgeConf, location, upstream)
		}
	}
}

func TestDevelopmentComposeStillBuildsFromSource(t *testing.T) {
	c := loadCompose(t, "docker-compose.yml")
	for _, name := range []string{"backend", "frontend", "seed"} {
		svc, ok := c.Services[name]
		if !ok || svc.Build == nil || strings.HasPrefix(svc.Image, "ghcr.io/") {
			t.Errorf("docker-compose.yml no longer builds %s from the checkout", name)
		}
	}
}

func TestDeployRunbookNamesEveryImageAndTheComposeFile(t *testing.T) {
	runbook := readRepoFile(t, deployRunbook)
	for name := range productionImages {
		if !strings.Contains(runbook, imageOwner+name) {
			t.Errorf("%s does not name %s%s", deployRunbook, imageOwner, name)
		}
	}
	for _, want := range []string{releaseCompose, gatewayEnv, migrateEnv, "deploy/store/bootstrap.sql"} {
		if !strings.Contains(runbook, want) {
			t.Errorf("%s does not name %s", deployRunbook, want)
		}
	}
	readme := readRepoFile(t, "README.md")
	if !strings.Contains(readme, imageWorkflow) || !strings.Contains(readme, deployRunbook) {
		t.Errorf("README.md does not point at %s and %s", imageWorkflow, deployRunbook)
	}
}

func isInlineSecret(key string) bool {
	return strings.HasSuffix(key, "_PASSWORD") || key == "AVIOR_SESSION_SECRET" || key == "PGPASSWORD"
}
