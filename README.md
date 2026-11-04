# tools

Host-side command-line utilities used by the [ps2build](https://github.com/ps2build/ps2build) build and install pipeline. `ps2build` resolves each tool from a `$PS2DEV/tools` install, so no `PATH` setup is needed.

| Tool | Purpose |
|---|---|
| [`bin2c`](cmd/bin2c) | Converts a binary file into a C header containing a byte array and its size. |
| [`adpenc`](cmd/adpenc) | Converts a WAV file to PS2 ADPCM (`.adp`), with optional loop points. |
| [`ps2pack`](cmd/ps2pack) | Compresses a PS2 ELF into a self-decompressing executable and reports the compression ratio. |
| [`ps2pkgconf`](cmd/ps2pkgconf) | pkg-config-compatible resolver over `package.yaml` manifests, sufficient for CMake's `FindPkgConfig` against an installed SDK. |
| [`dvp-as`](cmd/dvp-as) | Assembles `.vsm` VU microcode sources into relocatable ELF objects. See the `internal/dvpas` package documentation for supported syntax. |
| [`masp`](cmd/masp) | Expands macros, conditionals, repeats and includes (`.macro`, `.aif`, `.arepeat`, `.include`) in assembly source before it reaches `vcl` or an assembler. |
| [`ps2client`](cmd/ps2client) | Controls a PS2 running ps2link: reset, execee, execiop, dumpmem, writemem, dumpreg, serving `host:` file requests from a directory, and console output. |
| [`srxfixup`](cmd/srxfixup) | Converts and strips EE and IOP executables (`eefixup`, `iopfixup`, `erx-strip`, `irx-strip` are the same binary under different names). |

## Development

```
go build ./cmd/...
go vet ./...
gofmt -l .
go test ./...
```

Third-party Go dependency licenses are bundled with each release under `licenses/tools/`.

## Credit

Based on the tools of [ps2dev/ps2sdk](https://github.com/ps2dev/ps2sdk).

## License

CC0 1.0. See [LICENSE](LICENSE).
