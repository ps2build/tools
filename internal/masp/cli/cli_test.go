package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vector struct {
	Name           string            `json:"name"`
	Argv           []string          `json:"argv"`
	Files          map[string]string `json:"files"`
	Input          string            `json:"input"`
	HasInputFile   bool              `json:"has_input_file"`
	OutputMode     string            `json:"output_mode"`
	OutputFile     string            `json:"output_file"`
	ExpectedOutput string            `json:"expected_output"`
	ExpectedStdout string            `json:"expected_stdout"`
	ExpectedExit   int               `json:"expected_exit"`
}

func latin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}

func TestVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			dir := t.TempDir()
			cwd := filepath.ToSlash(dir)
			files := map[string]string{}
			for name, content := range v.Files {
				files[name] = content
			}
			if v.HasInputFile {
				files["in.vcl"] = v.Input
			}
			for name, content := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				body := strings.ReplaceAll(content, "{CWD}", cwd)
				if err := os.WriteFile(path, []byte(latin1(body)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chdir(orig) })

			var stdout, stderr bytes.Buffer
			status := Run(append([]string{"masp"}, v.Argv...), &stdout, &stderr)

			var got, wantOut string
			var gotStdout, wantStdout string
			if v.OutputMode == "stdout" {
				got, wantOut = stdout.String(), v.ExpectedOutput
			} else {
				out, _ := os.ReadFile(filepath.Join(dir, v.OutputFile))
				got, wantOut = string(out), v.ExpectedOutput
				gotStdout, wantStdout = stdout.String(), v.ExpectedStdout
			}
			norm := func(s string) string { return strings.ReplaceAll(s, cwd, "{CWD}") }
			if norm(got) != latin1(wantOut) {
				t.Errorf("output mismatch\n got: %q\nwant: %q", norm(got), latin1(wantOut))
			}
			if gotStdout != latin1(wantStdout) {
				t.Errorf("stdout mismatch\n got: %q\nwant: %q", gotStdout, latin1(wantStdout))
			}
			if status != v.ExpectedExit {
				t.Errorf("exit status = %d, want %d (stderr %q)", status, v.ExpectedExit, stderr.String())
			}
		})
	}
}
