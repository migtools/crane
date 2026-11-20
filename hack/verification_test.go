package hack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestUnitPackages(t *testing.T) {
	script, err := filepath.Abs("unit-packages.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, packages, want string
		status               string
		wantError            bool
	}{
		{
			name: "exclude only live suite subtree",
			packages: "example/crane/cmd/export\nexample/crane/e2e-tests/config\n" +
				"example/crane/e2e-tests/framework\nexample/crane/e2e-tests/utils\n" +
				"example/crane/e2e-tests/tests\nexample/crane/e2e-tests/tests/tier0\n" +
				"example/crane/e2e-tests/tests/tier1\nexample/crane/e2e-tests/tests/tier2\n" +
				"example/crane/e2e-tests/tests-extra\n",
			want: "example/crane/cmd/export\nexample/crane/e2e-tests/config\n" +
				"example/crane/e2e-tests/framework\nexample/crane/e2e-tests/utils\n" +
				"example/crane/e2e-tests/tests-extra\n",
		},
		{name: "discovery failure with partial output", packages: "example/crane/cmd/export\n", status: "17", wantError: true},
		{name: "empty discovery", wantError: true},
		{name: "only live suites", packages: "example/crane/e2e-tests/tests/tier2\n", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			goBin := filepath.Join(dir, "custom go")
			status := tc.status
			if status == "" {
				status = "0"
			}
			writeFile(t, goBin, "#!/bin/bash\n[[ $* == 'list ./...' ]] || exit 99\nprintf '%s' '"+tc.packages+"'\nexit "+status+"\n", 0700)
			output, err := run(t, dir, []string{"GO=" + goBin}, "bash", script)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, output = %s", err, output)
			}
			if !tc.wantError && output != tc.want {
				t.Fatalf("got %q, want %q", output, tc.want)
			}
		})
	}
}

func TestFmtCheck(t *testing.T) {
	script, err := filepath.Abs("fmt-check.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, contents string
		tracked        bool
		wantError      bool
	}{
		{name: "formatted tracked file", contents: "package sample\n", tracked: true},
		{name: "unformatted tracked file", contents: "package sample\nfunc f( ){ }\n", tracked: true, wantError: true},
		{name: "invalid Go file", contents: "not Go\n", tracked: true, wantError: true},
		{name: "ignore untracked file", contents: "not Go\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if output, err := run(t, dir, nil, "git", "init", "--quiet"); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			path := filepath.Join(dir, "file with spaces\nand newline.go")
			writeFile(t, path, tc.contents, 0600)
			if tc.tracked {
				if output, err := run(t, dir, nil, "git", "add", "--", path); err != nil {
					t.Fatalf("git add: %v: %s", err, output)
				}
			}
			output, err := run(t, dir, nil, "bash", script)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, output = %s", err, output)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != tc.contents {
				t.Fatal("fmt-check modified the file")
			}
		})
	}
	t.Run("git failure", func(t *testing.T) {
		if output, err := run(t, t.TempDir(), nil, "bash", script); err == nil {
			t.Fatalf("expected failure outside a Git repository, got %s", output)
		}
	})
}

func TestMakeCommands(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, target, failCall string
		args                   []string
		wantCalls              []string
	}{
		{
			name: "build all packages and binary", target: "build",
			wantCalls: []string{"build ./...", "build -o crane ."},
		},
		{
			name: "default unit tests", target: "test-unit",
			wantCalls: []string{"list ./...", "test example/crane/cmd/export"},
		},
		{
			name: "unit coverage and test arguments", target: "test-unit",
			args:      []string{"GO_TEST_ARGS=-v -count=1", "COVERAGE_PROFILE=coverage.out"},
			wantCalls: []string{"list ./...", "test -v -count=1 -coverprofile=coverage.out -covermode=atomic example/crane/cmd/export"},
		},
		{
			name: "coverage mode override", target: "test-unit",
			args:      []string{"COVERAGE_PROFILE=coverage.out", "COVERAGE_MODE=count"},
			wantCalls: []string{"list ./...", "test -coverprofile=coverage.out -covermode=count example/crane/cmd/export"},
		},
		{
			name: "compile all suites without tests or benchmarks", target: "test-e2e-compile",
			args:      []string{"GO_TEST_ARGS=-v -count=1"},
			wantCalls: []string{"test -v -count=1 -run ^$ -bench ^$ ./e2e-tests/tests/..."},
		},
		{name: "package build failure", target: "build", failCall: "build ./..."},
		{name: "binary build failure", target: "build", failCall: "build -o crane ."},
		{name: "discovery failure", target: "test-unit", failCall: "list ./..."},
		{name: "test execution failure", target: "test-unit", failCall: "test example/crane/cmd/export"},
		{name: "suite compile failure", target: "test-e2e-compile", failCall: "test -run ^$ -bench ^$ ./e2e-tests/tests/..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			goBin := filepath.Join(dir, "custom go")
			log := filepath.Join(dir, "calls")
			writeFile(t, goBin, `#!/bin/bash
printf '%s\n' "$*" >> "$CALL_LOG"
if [[ $* == "$FAIL_CALL" ]]; then
  echo 'intentional Go failure' >&2
  exit 17
fi
if [[ $1 == list ]]; then
  printf '%s\n' example/crane/cmd/export example/crane/e2e-tests/tests/tier2
fi
`, 0700)
			// Parent make exports command-line variables to recursive invocations.
			args := append([]string{tc.target, "GO=" + goBin, "GO_TEST_ARGS=", "COVERAGE_PROFILE=", "COVERAGE_MODE=atomic"}, tc.args...)
			output, err := run(t, root, []string{"CALL_LOG=" + log, "FAIL_CALL=" + tc.failCall}, "make", args...)
			if tc.failCall != "" {
				if err == nil || !strings.Contains(output, "intentional Go failure") {
					t.Fatalf("expected Go failure, got %v: %s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("make: %v: %s", err, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Join(tc.wantCalls, "\n") + "\n"; string(calls) != want {
				t.Fatalf("Go calls = %q, want %q", calls, want)
			}
		})
	}
}
