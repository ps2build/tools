package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ps2build/tools/internal/masp"
)

const (
	program = "masp"
	version = "0.2.0"
	usage   = `usage: masp [options] [input-file ...]
  -c, --commentchar CHAR   comment character (default !)
  -o, --output FILE        write the result to FILE (default: standard output)
  -I, --include DIR        add DIR to the include search path
  -D NAME[=VALUE]          define the numeric variable NAME (default value 1)
  -P, --prefixchar CHAR    directive prefix character (default .)
  -s, --copysource         copy each source line to the output as a comment
  -p, --print              number the copied source lines (with -s)
  -u, --unreasonable       lift the macro expansion limit
  -l, --line-numbers       accepted for compatibility
  -d, --debug              accepted for compatibility
  -h, --help               print this message
  -v, --version            print the version
`
)

type option struct {
	short byte
	long  string
	arg   bool
}

var options = []option{
	{'c', "commentchar", true},
	{'o', "output", true},
	{'I', "include", true},
	{'D', "", true},
	{'P', "prefixchar", true},
	{'s', "copysource", false},
	{'p', "print", false},
	{'u', "unreasonable", false},
	{'l', "line-numbers", false},
	{'d', "debug", false},
	{'a', "alternate", false},
	{'M', "mri", false},
	{'h', "help", false},
	{'v', "version", false},
	{0, "define", true},
}

type parsed struct {
	opts   masp.Options
	output string
	files  []string
}

func Run(args []string, stdout, stderr io.Writer) int {
	p, code, done := parse(args[1:], stdout, stderr)
	if done {
		return code
	}
	return execute(p, stdout, stderr)
}

func findShort(c byte) (option, bool) {
	for _, o := range options {
		if o.short == c {
			return o, true
		}
	}
	return option{}, false
}

func findLong(name string) (option, bool) {
	var matches []option
	for _, o := range options {
		if o.long == "" {
			continue
		}
		if o.long == name {
			return o, true
		}
		if strings.HasPrefix(o.long, name) {
			matches = append(matches, o)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return option{}, false
}

func parse(args []string, stdout, stderr io.Writer) (parsed, int, bool) {
	var p parsed
	apply := func(o option, value string) (int, bool) {
		switch o.short {
		case 'c':
			p.opts.SetComment = true
			p.opts.CommentChar = firstByte(value)
		case 'o':
			p.output = value
		case 'I':
			p.opts.IncludeDirs = append(p.opts.IncludeDirs, value)
		case 'D':
			p.opts.Defines = append(p.opts.Defines, value)
		case 'P':
			p.opts.SetPrefix = true
			p.opts.Prefix = firstByte(value)
		case 's':
			p.opts.CopySource = true
		case 'p':
			p.opts.LineNumbers = true
		case 'u':
			p.opts.Unreasonable = true
		case 'h':
			fmt.Fprint(stdout, usage)
			return 0, true
		case 'v':
			fmt.Fprintln(stdout, program, version)
			return 0, true
		}
		return 0, false
	}
	unknown := func(name string) (parsed, int, bool) {
		fmt.Fprintf(stderr, "%s: unknown option -- %s\n%s", program, name, usage)
		return p, 1, true
	}
	missing := func(name string) (parsed, int, bool) {
		fmt.Fprintf(stderr, "%s: option requires an argument -- %s\n%s", program, name, usage)
		return p, 1, true
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			p.files = append(p.files, args[i+1:]...)
			return p, 0, false
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(a[2:], "=")
			o, ok := findLong(name)
			if !ok {
				return unknown(name)
			}
			if o.arg && !hasValue {
				if i+1 >= len(args) {
					return missing(name)
				}
				i++
				value = args[i]
			}
			if code, done := apply(o, value); done {
				return p, code, true
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				o, ok := findShort(a[j])
				if !ok || o.short == 0 {
					return unknown(string(a[j]))
				}
				value := ""
				if o.arg {
					if j+1 < len(a) {
						value = a[j+1:]
					} else if i+1 < len(args) {
						i++
						value = args[i]
					} else {
						return missing(string(a[j]))
					}
				}
				if code, done := apply(o, value); done {
					return p, code, true
				}
				if o.arg {
					break
				}
			}
		default:
			p.files = append(p.files, a)
		}
	}
	return p, 0, false
}

func firstByte(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

func execute(p parsed, stdout, stderr io.Writer) int {
	var out io.Writer = stdout
	if p.output != "" {
		f, err := os.Create(p.output)
		if err != nil {
			fmt.Fprintf(stderr, "%s: Can't open output file `%s'.\n", program, p.output)
			return 1
		}
		defer f.Close()
		out = f
	}

	p.opts.SharedStdout = p.output == ""
	p.opts.SharedStdout = p.output == ""
	proc := masp.New(p.opts)
	status := 0
	for _, name := range p.files {
		data, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: Can't open input file `%s'.\n", program, name)
			status = 1
			break
		}
		proc.File(name, data)
	}

	res := proc.Result()
	out.Write(res.Output)
	stdout.Write(res.Stdout)
	stderr.Write(res.Stderr)
	if res.Status != 0 {
		status = res.Status
	}
	return status
}
