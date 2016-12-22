package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

// CheckoutIndexOptions represents the options that may be passed to
// "git checkout-index"
type CheckoutIndexOptions struct {
	UpdateStat bool

	Quiet bool
	Force bool
	All   bool

	NoCreate bool

	Prefix string

	// Stage not implemented
	Stage string // <number>|all

	// Temp not implemented
	Temp bool

	// Stdin implies checkout-index with the --stdin parameter.
	// nil implies it wasn't passed.
	// (Which is a moot point, because --stdin isn't implemented)
	Stdin         io.Reader // nil implies no --stdin param passed
	NullTerminate bool
}

// Implements the "git checkout-index" subcommand.
func CheckoutIndex(c *Client, opts CheckoutIndexOptions, files []string) error {
	if len(files) != 0 && opts.All {
		return fmt.Errorf("Can not mix --all and named files")
	}

	idx, err := c.GitDir.ReadIndex()
	if err != nil {
		return err
	}
	if opts.All {
		for _, entry := range idx.Objects {
			files = append(files, entry.PathName)
		}
	}

	for _, entry := range idx.Objects {
		for _, file := range files {
			indexpath, err := File(file).IndexPath(c)
			if err != nil {
				if !opts.Quiet {
					fmt.Fprintf(os.Stderr, "%v\n", err)
				}
				continue

			}

			if entry.PathName != indexpath.String() {
				continue
			}

			f := File(opts.Prefix + file)
			obj, err := c.GetObject(entry.Sha1)
			if f.Exists() && !opts.Force {
				if !opts.Quiet {
					fmt.Fprintf(os.Stderr, "%v already exists, no checkout\n", indexpath)
				}
				continue
			}
			if err != nil {
				return err
			}

			if !opts.NoCreate {
				fmode := os.FileMode(entry.Mode)
				err := ioutil.WriteFile(f.String(), obj.GetContent(), fmode)
				if err != nil {
					return err
				}
				os.Chmod(file, os.FileMode(entry.Mode))
			}

			// Update the stat information, but only if it's the same
			// file name. We only change the mtime, because the only
			// other thing we track is the file size, and that couldn't
			// have changed.
			// Don't change the stat info if there's a prefix, because
			// if we're checkout out into a prefix, it means we haven't
			// touched the index.
			if opts.UpdateStat && opts.Prefix == "" {
				fstat, err := f.Stat()
				if err != nil {
					return err
				}

				modTime := fstat.ModTime()
				entry.Mtime = uint32(modTime.Unix())
				entry.Mtimenano = uint32(modTime.Nanosecond())
			}
		}
	}

	if opts.UpdateStat {
		f, err := c.GitDir.Create(File("index"))
		if err != nil {
			return err
		}
		defer f.Close()
		return idx.WriteIndex(f)

	}
	return nil
}

// Parses the command arguments from args (usually from os.Args) into a
// CheckoutIndexOptions and calls CheckoutIndex.
func CheckoutIndexCmd(c *Client, args []string) error {
	flags := flag.NewFlagSet("checkout-index", flag.ExitOnError)
	options := CheckoutIndexOptions{}

	index := flags.Bool("index", false, "Update stat information for checkout out entries in the index")
	u := flags.Bool("u", false, "Alias for --index")

	quiet := flags.Bool("quiet", false, "Be quiet if files exist or are not in index")
	q := flags.Bool("q", false, "Alias for --quiet")

	force := flags.Bool("force", false, "Force overwrite of existing files")
	f := flags.Bool("f", false, "Alias for --force")

	all := flags.Bool("all", false, "Checkout all files in the index.")
	a := flags.Bool("a", false, "Alias for --all")

	nocreate := flags.Bool("no-create", false, "Don't checkout new files, only refresh existing ones")
	n := flags.Bool("n", false, "Alias for --no-create")

	flags.StringVar(&options.Prefix, "prefix", "", "When creating files, prepend string")
	flags.StringVar(&options.Stage, "stage", "", "Copy files from named stage (unimplemented)")

	flags.BoolVar(&options.Temp, "temp", false, "Instead of copying files to a working directory, write them to a temp dir")

	stdin := flags.Bool("stdin", false, "Instead of taking paths from command line, read from stdin")
	flags.BoolVar(&options.NullTerminate, "z", false, "Use nil instead of newline to terminate paths read from stdin")

	flags.Parse(args)
	files := flags.Args()
	options.UpdateStat = *index || *u
	options.Quiet = *quiet || *q
	options.Force = *force || *f
	options.All = *all || *a
	options.NoCreate = *nocreate || *n
	if *stdin {
		options.Stdin = os.Stdin
	}

	return CheckoutIndex(c, options, files)

}
