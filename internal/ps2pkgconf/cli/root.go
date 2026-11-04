// Package cli implements enough of pkg-config's real CLI surface for
// CMake's FindPkgConfig.cmake module (pkg_check_modules) to work against
// ps2build's own installed package.yaml manifests - not a general
// pkg-config replacement, just the flags a typical upstream CMakeLists.txt
// actually invokes (confirmed against gsKit's own find_package(PkgConfig)
// + pkg_check_modules(LIBPNG libpng) usage).
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/ps2build/tools/internal/ps2pkgconf/pkg"
	"github.com/spf13/cobra"
)

var (
	flagExists          bool
	flagModVersion      bool
	flagCflags          bool
	flagCflagsOnlyI     bool
	flagCflagsOnlyOther bool
	flagLibs            bool
	flagLibsOnlyL       bool
	flagLibsOnlyl       bool
	flagLibsOnlyOther   bool
	// --print-errors/--silence-errors: accepted for CMake compatibility,
	// no behavior difference - this tool always writes its one error line
	// to stderr and exits 1, matching --print-errors' effect, since
	// FindPkgConfig.cmake's own error handling reads stderr either way.
	flagPrintErrors   bool
	flagSilenceErrors bool
	flagShortErrors   bool
)

var rootCmd = &cobra.Command{
	Use:     "ps2pkgconf <module...>",
	Short:   "pkg-config-compatible resolver over ps2build's own package.yaml manifests",
	Version: "0.1.0",
	// Long, not Version - real pkg-config/pkgconf's own --version prints
	// a bare version string, which CMake's FindPkgConfig.cmake parses
	// directly (see the comment on Version above). Adding anything else
	// to --version output would risk breaking that real integration, so
	// the license pointer goes in --help instead.
	Long: "pkg-config-compatible resolver over ps2build's own package.yaml manifests\n\n" +
		"Third-party licenses: see licenses/tools/ in the source repository (github.com/ps2build/tools).",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE:         run,
}

func Execute() error {
	// Real pkg-config/pkgconf has a much larger flag surface than this
	// tool implements (--uninstalled, --define-variable, --errors-to-
	// stdout, ...) - CMake's FindPkgConfig.cmake passes a handful of
	// purely cosmetic ones (found --short-errors this way, in addition
	// to --print-errors/--silence-errors already handled explicitly).
	// Tolerating any OTHER unrecognized flag rather than hard-erroring
	// on each one individually as they're found is more robust than
	// chasing them one at a time - this tool only needs to answer the
	// handful of query flags it actually implements correctly, not
	// reject every flag real pkg-config also accepts.
	rootCmd.FParseErrWhitelist.UnknownFlags = true

	rootCmd.Flags().BoolVar(&flagExists, "exists", false, "check that the given module(s) exist")
	rootCmd.Flags().BoolVar(&flagModVersion, "modversion", false, "print the version of the given module")
	rootCmd.Flags().BoolVar(&flagCflags, "cflags", false, "print compiler flags")
	rootCmd.Flags().BoolVar(&flagCflagsOnlyI, "cflags-only-I", false, "print -I flags only")
	rootCmd.Flags().BoolVar(&flagCflagsOnlyOther, "cflags-only-other", false, "print non -I compiler flags (always empty here)")
	rootCmd.Flags().BoolVar(&flagLibs, "libs", false, "print linker flags")
	rootCmd.Flags().BoolVar(&flagLibsOnlyL, "libs-only-L", false, "print -L flags only")
	rootCmd.Flags().BoolVar(&flagLibsOnlyl, "libs-only-l", false, "print -l flags only")
	rootCmd.Flags().BoolVar(&flagLibsOnlyOther, "libs-only-other", false, "print non -L/-l linker flags (always empty here)")
	rootCmd.Flags().BoolVar(&flagPrintErrors, "print-errors", false, "accepted for CMake compatibility, no effect")
	rootCmd.Flags().BoolVar(&flagSilenceErrors, "silence-errors", false, "accepted for CMake compatibility, no effect")
	rootCmd.Flags().BoolVar(&flagShortErrors, "short-errors", false, "accepted for CMake compatibility, no effect")
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) error {
	ps2dev := os.Getenv("PS2DEV")
	if ps2dev == "" {
		return fmt.Errorf("PS2DEV is not set")
	}

	manifests := make([]*pkg.Manifest, 0, len(args))
	for _, name := range args {
		m, ok, err := pkg.Find(ps2dev, name)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "Package %s was not found in the ps2build package tree ($PS2DEV/packages)\n", name)
			fmt.Fprintf(os.Stderr, "No package '%s' found\n", name)
			os.Exit(1)
		}
		manifests = append(manifests, m)
	}

	if flagExists {
		// Found every named module (Find already exited 1 above on the
		// first miss) - nothing to print, exit 0.
		return nil
	}

	if flagModVersion {
		for _, m := range manifests {
			fmt.Println(m.ModVersion())
		}
		return nil
	}

	var out []string
	addCflags := func() {
		for _, m := range manifests {
			if f, ok := m.CFlags(); ok {
				out = append(out, f...)
			}
		}
	}
	addLibDirs := func() {
		for _, m := range manifests {
			if f, ok := m.LibDir(); ok {
				out = append(out, f)
			}
		}
	}
	addLibNames := func() {
		for _, m := range manifests {
			if f, ok := m.LibName(); ok {
				out = append(out, f)
			}
		}
	}

	switch {
	case flagCflagsOnlyI:
		addCflags()
	case flagCflagsOnlyOther:
		// no non -I compiler flags in this scheme
	case flagCflags:
		addCflags()
	case flagLibsOnlyL:
		addLibDirs()
	case flagLibsOnlyl:
		addLibNames()
	case flagLibsOnlyOther:
		// no non -L/-l linker flags in this scheme
	case flagLibs:
		addLibDirs()
		addLibNames()
	default:
		// Real pkg-config's default action (no --cflags/--libs/--modversion etc given) is a silent existence check, exit 0 if every named module was found - not an error.
		return nil
	}

	fmt.Println(strings.Join(out, " "))
	return nil
}
