package cli

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ps2build/tools/pkg/ps2client/netfs"
)

func (o *options) fsCommand() *cobra.Command {
	root := &cobra.Command{Use: "fs", Short: "Work with the PS2's file systems over the network"}
	with := func(fn func(c *netfs.Client, a []string) error) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, a []string) error {
			c, err := netfs.Dial(o.hostName(), o.idle(10*time.Second))
			if err != nil {
				return err
			}
			defer c.Close()
			return fn(c, a)
		}
	}
	simple := func(use, short string, nargs int, fn func(c *netfs.Client, a []string) error) {
		root.AddCommand(&cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(nargs), RunE: with(fn)})
	}

	root.AddCommand(&cobra.Command{
		Use: "dir <path>", Short: "List a directory on the PS2", Args: cobra.ExactArgs(1),
		RunE: with(func(c *netfs.Client, a []string) error {
			list, err := c.List(a[0])
			if err != nil {
				return err
			}
			for _, e := range list {
				kind := "-"
				if e.IsDir() {
					kind = "d"
				}
				when := "                   "
				if !e.Modified.IsZero() {
					when = e.Modified.Format("2006-01-02 15:04:05")
				}
				fmt.Printf("%s %10d %s %s\n", kind, e.Size, when, e.Name)
			}
			return nil
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "copyfrom <remote> [local]", Short: "Copy a file from the PS2 to this computer", Args: cobra.RangeArgs(1, 2),
		RunE: with(func(c *netfs.Client, a []string) error {
			dst := path.Base(strings.ReplaceAll(a[0], ":", "/"))
			if len(a) == 2 {
				dst = a[1]
			}
			f, err := os.Create(dst)
			if err != nil {
				return err
			}
			n, err := c.Download(a[0], f)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(dst)
				return err
			}
			o.logf("copied %d bytes to %s", n, dst)
			return nil
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "copyto <local> <remote>", Short: "Copy a file from this computer to the PS2", Args: cobra.ExactArgs(2),
		RunE: with(func(c *netfs.Client, a []string) error {
			f, err := os.Open(a[0])
			if err != nil {
				return err
			}
			defer f.Close()
			n, err := c.Upload(a[1], f)
			if err != nil {
				return err
			}
			o.logf("copied %d bytes to %s", n, a[1])
			return nil
		}),
	})
	simple("delete <path>", "Delete a file on the PS2", 1, func(c *netfs.Client, a []string) error { return c.Remove(a[0]) })
	simple("mkdir <path>", "Create a directory on the PS2", 1, func(c *netfs.Client, a []string) error { return c.Mkdir(a[0]) })
	simple("rmdir <path>", "Remove an empty directory on the PS2", 1, func(c *netfs.Client, a []string) error { return c.Rmdir(a[0]) })
	simple("umount <device>", "Unmount a device on the PS2", 1, func(c *netfs.Client, a []string) error { return c.Umount(a[0]) })
	root.AddCommand(&cobra.Command{
		Use: "sync <device> [flag]", Short: "Flush pending writes on a device", Args: cobra.RangeArgs(1, 2),
		RunE: with(func(c *netfs.Client, a []string) error {
			var flag int64
			if len(a) == 2 {
				var err error
				if flag, err = strconv.ParseInt(a[1], 0, 32); err != nil {
					return fmt.Errorf("flag: %w", err)
				}
			}
			return c.Sync(a[0], int32(flag))
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "mount <driver> <block-device> [flags]", Short: "Mount a block device on a file system driver", Args: cobra.RangeArgs(2, 3),
		RunE: with(func(c *netfs.Client, a []string) error {
			var flags int64
			if len(a) == 3 {
				var err error
				if flags, err = strconv.ParseInt(a[2], 0, 32); err != nil {
					return fmt.Errorf("flags: %w", err)
				}
			}
			return c.Mount(a[0], a[1], int32(flags), nil)
		}),
	})
	root.AddCommand(&cobra.Command{
		Use: "devlist", Short: "List the devices available on the PS2", Args: cobra.NoArgs,
		RunE: with(func(c *netfs.Client, _ []string) error {
			devs, err := c.Devices()
			if err != nil {
				return err
			}
			for _, d := range devs {
				fmt.Println(d)
			}
			return nil
		}),
	})
	return root
}
