package main

import (
	"fmt"
	"os"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if len(os.Args) != 4 {
		fmt.Println("bin2c - from bin2s By Sjeep")
		fmt.Println("Usage: bin2c infile outfile label")
		os.Exit(1)
	}

	src, dest, label := os.Args[1], os.Args[2], os.Args[3]

	srcData, err := os.ReadFile(src)
	check(err)

	w, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	check(err)

	fmt.Fprintf(w, "#ifndef __%s__\n", label)
	fmt.Fprintf(w, "#define __%s__\n\n", label)
	fmt.Fprintf(w, "unsigned int size_%s = %d;\n", label, len(srcData))
	fmt.Fprintf(w, "unsigned char %s[] __attribute__((aligned(16))) = {", label)

	for i, b := range srcData {
		if i%16 == 0 {
			fmt.Fprint(w, "\n\t")
		}
		fmt.Fprintf(w, "0x%02x, ", b)
	}

	fmt.Fprint(w, "\n};\n\n#endif\n")

	check(w.Close())
}
