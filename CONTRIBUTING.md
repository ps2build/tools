# Contributing

## Workflow

1. Open an issue before adding a tool or changing a tool's command-line interface.
2. Fork, branch from `main`, and keep each pull request to one change.
3. Run `gofmt -l .`, `go vet ./...` and `go test ./...` before pushing; CI runs the same checks plus a license policy check.
4. If `go.mod` changes, run `.github/scripts/generate-licenses.sh` and commit `licenses/`.

## Code style

- Comments are single lines and only for tool, compiler or linker behavior that is not obvious from the code. Tests are documented by their names.
- CLI help text states what a command accomplishes, not how it works internally.

## AI-assisted contributions

AI-generated code is accepted. Contributions that the author cannot explain without AI assistance are discouraged and may be closed.

- State in the pull request which AI tool and model were used, or that none was.
- Write the pull request description yourself, without AI.
- You are responsible for every line you submit: be prepared to explain and defend each change in review.

## License

By submitting a contribution, you agree that it is licensed under the same license as this repository (see [LICENSE](LICENSE)).
