// Package cli implements srxfixup's command line.
package cli

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ps2build/tools/internal/srxfixup/config"
	"github.com/ps2build/tools/internal/srxfixup/convert"
	"github.com/ps2build/tools/internal/srxfixup/elf"
)

func Execute() error {
	progname := os.Args[0]
	base := progname
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	stripMode := len(base) > 5 && base[len(base)-5:] == "strip"

	fs := flag.NewFlagSet(progname, flag.ContinueOnError)
	verbose := fs.Bool("v", false, "")
	ofile := fs.String("o", "", "")
	rfile := fs.String("r", "", "")
	ffile := fs.String("f", "", "")
	startaddr := fs.String("t", "", "")
	entrysym := fs.String("e", "", "")
	dispmod := fs.Bool("m", false, "")
	irx1 := fs.Bool("irx1", false, "")
	rb1 := fs.Bool("rb", false, "")
	rb2 := fs.Bool("relative-branch", false, "")
	allowZeroText := fs.Bool("allow-zero-text", false, "")
	fs.Usage = func() { usage(progname, stripMode, *verbose) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	brConv := *rb1 || *rb2

	args := fs.Args()
	if len(args) == 0 {
		usage(progname, stripMode, *verbose)
		return fmt.Errorf("no input file")
	}
	if len(args) > 1 {
		usage(progname, stripMode, *verbose)
		return fmt.Errorf("too many input files")
	}
	source := args[0]
	if stripMode && *ofile == "" {
		*ofile = source
	}

	f, err := elf.ReadFile(source)
	if err != nil {
		return err
	}

	var conf *config.Table
	if (f.Ehdr.Flags&elf.EFMipsMach) == elf.EFMipsMach5900 && (f.Ehdr.Flags&elf.EFMipsArch) == elf.EFMipsArch3 {
		conf = config.EE
	} else {
		conf = config.IOP
	}

	switch f.Ehdr.Type {
	case elf.ETRel:
		if !*allowZeroText && convert.CheckZeroTextSymbols(source, f) {
			return fmt.Errorf("zero-text symbol check failed")
		}
		needOutput := *rfile != "" || *ofile != "" || *ffile != ""
		if err := convert.ConvertRel2Srx(f, conf, *entrysym, needOutput, *irx1); err != nil {
			return err
		}
	case elf.ETSceIopRelExec, elf.ETSceIopRelExec2, elf.ETSceEeRelExec2, elf.ETExec:
	default:
		return fmt.Errorf("%q: unsupported ELF type (0x%x)", source, f.Ehdr.Type)
	}

	if *dispmod {
		displayModuleInfo(f)
	}

	switch f.Ehdr.Type {
	case elf.ETSceIopRelExec, elf.ETSceIopRelExec2, elf.ETSceEeRelExec2:
		if brConv {
			convert.ConvertRelativeBranch(f)
		}
		if *rfile != "" {
			if err := convert.LayoutSrxFile(f, conf); err != nil {
				return err
			}
			if err := elf.WriteFile(f, *rfile); err != nil {
				return err
			}
		}
		if *ofile != "" {
			convert.StripElf(f)
			if err := convert.LayoutSrxFile(f, conf); err != nil {
				return err
			}
			if err := elf.WriteFile(f, *ofile); err != nil {
				return err
			}
		}
	default:
		if *rfile != "" || *ofile != "" {
			return fmt.Errorf("cannot generate IRX/ERX file: %q file type is ET_EXEC", source)
		}
	}

	if *ffile != "" || *startaddr != "" {
		addr := uint32(0)
		if *startaddr != "" {
			v, err := strconv.ParseUint(*startaddr, 16, 32)
			if err != nil {
				return fmt.Errorf("invalid -t address %q: %w", *startaddr, err)
			}
			addr = uint32(v)
		}
		switch f.Ehdr.Type {
		case elf.ETSceIopRelExec, elf.ETSceIopRelExec2, elf.ETSceEeRelExec2:
			f.Ehdr.Type = elf.ETExec
			convert.FixLocationElf(f, addr)
		}
		if *ffile != "" {
			if err := convert.LayoutSrxFile(f, conf); err != nil {
				return err
			}
			if err := elf.WriteFile(f, *ffile); err != nil {
				return err
			}
		}
	}

	return nil
}

func displayModuleInfo(f *elf.File) {
	if s := f.SearchSection(elf.SHTSceIopMod); s != nil {
		m := s.IopMod()
		if m.ModuleInfo != 0xFFFFFFFF {
			fmt.Printf("name:%s version:%d.%d\n", m.ModuleName, byte(m.ModuleVersion>>8), byte(m.ModuleVersion))
		}
	}
	if s := f.SearchSection(elf.SHTSceEeMod); s != nil {
		m := s.EeMod()
		if m.ModuleInfo != 0xFFFFFFFF {
			fmt.Printf("name:%s version:%d.%d\n", m.ModuleName, byte(m.ModuleVersion>>8), byte(m.ModuleVersion))
		}
	}
}

func usage(myname string, stripMode, verbose bool) {
	if stripMode {
		fmt.Printf("%s\nusage: %s [options] <elf_file>\n", myname, myname)
		fmt.Print("  options:\n" +
			"    -v\n" +
			"    -m\n" +
			"    -o <elf_relocatable_nosymbol_output_file>\n" +
			"    --relative-branch  or  --rb\n")
		return
	}
	fmt.Printf("IOP/EE relocatable object converter\n%s\nusage: %s [options] <elf_input_file>\n", myname, myname)
	fmt.Print("  options:\n" +
		"    -v\n" +
		"    -m\n" +
		"    --irx1\n" +
		"    -o <elf_relocatable_nosymbol_output_file>\n" +
		"    -r <elf_relocatable_output_file>\n" +
		"    -e <entry_point_symbol>\n" +
		"    --relative-branch  or  --rb\n" +
		"    --allow-zero-text  (skip check for .text symbols with value 0)\n")
	if verbose {
		fmt.Print("    -t <.text start address>\n" +
			"    -f <elf_fixedaddress_output_file>\n")
	}
}
