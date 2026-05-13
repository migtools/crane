package kustomize

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAndValidateArgs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string
		expectError bool
		errorMsg    string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single flag",
			input:    "--enable-helm",
			expected: []string{"--enable-helm"},
		},
		{
			name:     "multiple flags",
			input:    "--enable-helm --enable-alpha-plugins",
			expected: []string{"--enable-helm", "--enable-alpha-plugins"},
		},
		{
			name:     "flag with value using equals",
			input:    "--load-restrictor=LoadRestrictionsNone",
			expected: []string{"--load-restrictor=LoadRestrictionsNone"},
		},
		{
			name:     "helm command with path",
			input:    "--helm-command=/usr/local/bin/helm",
			expected: []string{"--helm-command=/usr/local/bin/helm"},
		},
		{
			name:     "env with quoted value",
			input:    "--env 'FOO=bar'",
			expected: []string{"--env", "FOO=bar"},
		},
		{
			name:     "env short form",
			input:    "-e FOO=bar",
			expected: []string{"-e", "FOO=bar"},
		},
		{
			name:     "multiple env vars",
			input:    "--env FOO=bar --env BAZ=qux",
			expected: []string{"--env", "FOO=bar", "--env", "BAZ=qux"},
		},
		{
			name:     "env with equals in flag",
			input:    "--env=FOO=bar",
			expected: []string{"--env=FOO=bar"},
		},
		{
			name:     "complex combination",
			input:    "--enable-helm --load-restrictor=LoadRestrictionsNone --env FOO=bar",
			expected: []string{"--enable-helm", "--load-restrictor=LoadRestrictionsNone", "--env", "FOO=bar"},
		},
		{
			name:     "load-restrictor space-separated",
			input:    "--load-restrictor LoadRestrictionsNone",
			expected: []string{"--load-restrictor", "LoadRestrictionsNone"},
		},
		{
			name:     "helm-command space-separated",
			input:    "--helm-command /usr/local/bin/helm",
			expected: []string{"--helm-command", "/usr/local/bin/helm"},
		},
		{
			name:        "disallowed flag",
			input:       "--some-random-flag",
			expectError: true,
			errorMsg:    "not allowed",
		},
		{
			name:        "command injection with semicolon",
			input:       "--enable-helm; rm -rf /",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "command injection with pipe",
			input:       "--enable-helm | cat /etc/passwd",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "command injection with ampersand",
			input:       "--enable-helm && rm -rf /",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "command injection with backtick",
			input:       "--helm-command=`rm -rf /`",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "command injection with dollar",
			input:       "--helm-command=$(rm -rf /)",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "command injection in env value",
			input:       "--env 'FOO=bar; rm -rf /'",
			expectError: true,
			errorMsg:    "forbidden characters",
		},
		{
			name:        "env without value",
			input:       "--env",
			expectError: true,
			errorMsg:    "requires a value",
		},
		{
			name:        "env with empty value",
			input:       "--env=",
			expectError: true,
			errorMsg:    "empty value",
		},
		{
			name:        "env followed by another flag",
			input:       "--env --enable-helm",
			expectError: true,
			errorMsg:    "requires a value, got flag",
		},
		{
			name:        "helm-command followed by another flag",
			input:       "--helm-command --enable-helm",
			expectError: true,
			errorMsg:    "requires a value, got flag",
		},
		{
			name:        "load-restrictor followed by another flag",
			input:       "--load-restrictor --enable-helm",
			expectError: true,
			errorMsg:    "requires a value, got flag",
		},
		{
			name:        "load-restrictor without value",
			input:       "--load-restrictor",
			expectError: true,
			errorMsg:    "requires a value",
		},
		{
			name:        "helm-command with empty value",
			input:       "--helm-command=",
			expectError: true,
			errorMsg:    "empty value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAndValidateArgs(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single arg",
			input:    "--flag",
			expected: []string{"--flag"},
		},
		{
			name:     "multiple args",
			input:    "--flag1 --flag2",
			expected: []string{"--flag1", "--flag2"},
		},
		{
			name:     "arg with value",
			input:    "--flag=value",
			expected: []string{"--flag=value"},
		},
		{
			name:     "single quoted string",
			input:    "--flag 'value with spaces'",
			expected: []string{"--flag", "value with spaces"},
		},
		{
			name:     "double quoted string",
			input:    `--flag "value with spaces"`,
			expected: []string{"--flag", "value with spaces"},
		},
		{
			name:     "multiple spaces",
			input:    "--flag1    --flag2",
			expected: []string{"--flag1", "--flag2"},
		},
		{
			name:     "quoted equals",
			input:    "--env 'FOO=bar baz'",
			expected: []string{"--env", "FOO=bar baz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitArgs(tt.input)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
