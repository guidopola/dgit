package cmd

import (
	"flag"
	"fmt"

	"github.com/driusan/dgit/git"
)

// Parses the arguments from git-cat-file as they were passed on the commandline
// and calls git.CatFiles
func CatFile(c *git.Client, args []string) error {
	flags := flag.NewFlagSet("cat-file", flag.ExitOnError)
	flags.SetOutput(flag.CommandLine.Output())
	flags.Usage = func() {
		flag.Usage()
		fmt.Fprintf(flag.CommandLine.Output(), "\n\nOptions:\n")
		flags.PrintDefaults()
	}
	options := git.CatFileOptions{}

	flags.BoolVar(&options.Pretty, "p", false, "Pretty print the object content")
	flags.BoolVar(&options.Size, "s", false, "Print the size of the object")
	flags.BoolVar(&options.Type, "t", false, "Print the type of the object")
	flags.BoolVar(&options.ExitCode, "e", false, "Exit with 0 status if file exists and is valid")
	flags.BoolVar(&options.AllowUnknownType, "allow-unknown-type", false, "Allow types that are unknown to git")
	flags.Parse(args)
	oargs := flags.Args()

	switch len(oargs) {
	case 0:
		flags.Usage()
		return nil
	case 1:
		shas, err := git.RevParse(c, git.RevParseOptions{}, oargs)
		if err != nil {
			return err
		}
		val, err := git.CatFile(c, "", shas[0].Id, options)
		if err != nil {
			return err
		}
		fmt.Print(val)
		return nil
	case 2:
		shas, err := git.RevParse(c, git.RevParseOptions{}, []string{oargs[1]})
		if err != nil {
			return err
		}
		val, err := git.CatFile(c, oargs[0], shas[0].Id, options)
		if err != nil {
			return err
		}
		fmt.Print(val)
		return nil
	default:
		flags.Usage()
	}
	return nil
}
