package git

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

// CheckoutOptions represents the options that may be passed to
// "git checkout"
type CheckoutOptions struct {
	// Not implemented
	Quiet bool
	// Not implemented
	Progress bool
	// Not implemented
	Force bool

	// Check out the named stage for unnamed paths.
	// Stage2 is equivalent to --ours, Stage3 to --theirs
	// Not implemented
	Stage Stage

	Branch string // -b

	// Not implemented
	ForceBranch bool // use branch as -B
	// Not implemented
	OrphanBranch bool // use branch as --orphan

	// Not implemented
	Track string
	// Not implemented
	CreateReflog bool // -l

	// Not implemented
	Detach bool

	IgnoreSkipWorktreeBits bool

	// Not implemented
	Merge bool

	// Not implemented.
	ConflictStyle string

	Patch bool

	// Not implemented
	IgnoreOtherWorktrees bool
}

// Implements the "git checkout" subcommand of git. Variations in the man-page
// are:
//
//     git checkout [-q] [-f] [-m] [<branch>]
//     git checkout [-q] [-f] [-m] --detach [<branch>]
//     git checkout [-q] [-f] [-m] [--detach] <commit>
//     git checkout [-q] [-f] [-m] [[-b|-B|--orphan] <new_branch>] [<start_point>]
//     git checkout [-f|--ours|--theirs|-m|--conflict=<style>] [<tree-ish>] [--] <paths>...
//     git checkout [-p|--patch] [<tree-ish>] [--] [<paths>...]
//
// This will just check the options and call the appropriate variation. You
// can avoid the overhead by calling the proper variation directly.
//
// "thing" is the thing that the user entered on the command line to be checked out. It
// might be a branch, a commit, or a treeish, depending on the variation above.
func Checkout(c *Client, opts CheckoutOptions, thing string, files []File) error {
	if thing == "" {
		thing = "HEAD"
	}

	if opts.Patch {
		diffs, err := DiffFiles(c, DiffFilesOptions{}, files)
		if err != nil {
			return err
		}
		var patchbuf bytes.Buffer
		if err := GeneratePatch(c, DiffCommonOptions{Patch: true}, diffs, &patchbuf); err != nil {
			return err
		}
		hunks, err := splitPatch(patchbuf.String(), false)
		if err != nil {
			return err
		}
		hunks, err = filterHunks("discard this hunk from the work tree", hunks)
		if err == userAborted {
			return nil
		} else if err != nil {
			return err
		}

		patch, err := ioutil.TempFile("", "checkoutpatch")
		if err != nil {
			return err
		}
		defer os.Remove(patch.Name())
		recombinePatch(patch, hunks)

		return Apply(c, ApplyOptions{Reverse: true}, []File{File(patch.Name())})
	}

	if len(files) == 0 {
		cmt, err := RevParseCommitish(c, &RevParseOptions{}, thing)
		if err != nil {
			return err
		}
		return CheckoutCommit(c, opts, cmt)
	}

	b, err := RevParseTreeish(c, &RevParseOptions{}, thing)
	if err != nil {
		return err
	}
	return CheckoutFiles(c, opts, b, files)
}

// Implements the "git checkout" subcommand of git for variations:
//     git checkout [-q] [-f] [-m] [<branch>]
//     git checkout [-q] [-f] [-m] --detach [<branch>]
//     git checkout [-q] [-f] [-m] [--detach] <commit>
//     git checkout [-q] [-f] [-m] [[-b|-B|--orphan] <new_branch>] [<start_point>]
func CheckoutCommit(c *Client, opts CheckoutOptions, commit Commitish) error {
	// RefSpec for new branch with -b/-B variety
	var newRefspec RefSpec
	if opts.Branch != "" {
		// Handle the -b/-B variety.
		// commit is the startpoint in the last variation, otherwise
		// Checkout() already set it to the commit of "HEAD"
		newRefspec = RefSpec("refs/heads/" + opts.Branch)
		refspecfile := newRefspec.File(c)
		if refspecfile.Exists() && !opts.ForceBranch {
			return fmt.Errorf("Branch %s already exists.", opts.Branch)
		}
	}
	// Get the original HEAD for the reflog
	var head Commitish
	head, err := SymbolicRefGet(c, SymbolicRefOptions{}, "HEAD")
	switch err {
	case DetachedHead:
		head, err = c.GetHeadCommit()
		if err != nil {
			return err
		}
	case nil:
	default:
		return err
	}

	// Convert from Commitish to Treeish for ReadTree and LsTree
	cid, err := commit.CommitID(c)
	if err != nil {
		return err
	}

	if !opts.Force {
		// Check that nothing would be lost
		lstree, err := LsTree(c, LsTreeOptions{Recurse: true}, cid, nil)
		if err != nil {
			return err
		}
		newfiles := make([]File, 0, len(lstree))
		for _, entry := range lstree {
			f, err := entry.PathName.FilePath(c)
			if err != nil {
				return err
			}
			newfiles = append(newfiles, f)
		}
		untracked, err := LsFiles(c, LsFilesOptions{Others: true}, newfiles)
		if err != nil {
			return err
		}
		if len(untracked) > 0 {
			err := "error: The following untracked working tree files would be overwritten by checkout:\n"
			for _, f := range untracked {
				err += "\t" + f.IndexEntry.PathName.String() + "\n"
			}
			err += "Please move or remove them before you switch branches.\nAborting"
			return fmt.Errorf("%v", err)
		}
	}

	// We don't pass update to ReadTree, because then we would lose our staged
	// changes. Instead, we get a list of modified files and after reading
	// the index do a CheckoutIndex on all the files that weren't different
	readtreeopts := ReadTreeOptions{Reset: true}
	if opts.Force {
		readtreeopts.Update = true
	}
	if opts.IgnoreSkipWorktreeBits {
		readtreeopts.NoSparseCheckout = true
	}

	// Get a diff on staged or modified files that should be left unmolested
	headtree, err := head.CommitID(c)
	if err != nil {
		return err
	}
	stageddiffs, err := DiffIndex(c, DiffIndexOptions{Cached: true}, nil, headtree, nil)
	if err != nil {
		return err
	}
	if !opts.Force {
		modifieddiffs, err := DiffFiles(c, DiffFilesOptions{}, nil)
		if err != nil {
			return err
		}
		if len(modifieddiffs) > 0 {
			var mfiles []string
			for _, m := range modifieddiffs {
				f, err := m.Name.FilePath(c)
				if err != nil {
					return err
				}
				mfiles = append(mfiles, f.String())
			}
			return fmt.Errorf(`error: Your local changes to the following files would be overwritten by checkout.
	%v
Please commit your changes or discard them before switching branches.`, strings.Join(mfiles, "\n\t"))
		}
	}

	// Keep a copy of the original index so that we can delete
	// removed files later.
	origidx, err := c.GitDir.ReadIndex()
	if err != nil {
		return err
	}
	// Read the new tree into memory.
	idx, err := ReadTree(c, readtreeopts, cid)
	if err != nil {
		return err
	}

	var checkoutfiles []File
newfiles:
	for _, obj := range idx.Objects {
		f, err := obj.PathName.FilePath(c)
		if err != nil {
			return err
		}

		if obj.SkipWorktree() && !opts.IgnoreSkipWorktreeBits {
			if f.Exists() {
				if err := os.Remove(f.String()); err != nil {
					return err
				}
			}
			continue newfiles
		}
		if !opts.Force {
			for _, staged := range stageddiffs {
				// Add the staged change back to the index, and don't
				// overwrite when switching branches. This doesn't apply
				// if a checkout/reset it forced.
				if err := idx.AddStage(c, staged.Name, staged.Dst.FileMode, staged.Dst.Sha1, Stage0, uint32(staged.DstSize), 0, UpdateIndexOptions{}); err != nil {
					return err
				}
				continue newfiles
			}
			checkoutfiles = append(checkoutfiles, f)
		}
	}

	// Write the index before checking out so that ls-files -k works.
	f, err := c.GitDir.Create("index")
	if err != nil {
		return err
	}
	defer f.Close()
	if err := idx.WriteIndex(f); err != nil {
		return err
	}

	// Delete any old files
	newidxmap := idx.GetMap()
	for _, obj := range origidx.Objects {
		if !newidxmap.Contains(obj.PathName) {
			if obj.PathName.IsClean(c, obj.Sha1) {
				f, err := obj.PathName.FilePath(c)
				if err != nil {
					return err
				}
				if err := os.RemoveAll(f.String()); err != nil {
					return err
				}
			}
		}
	}

	// Now update the files on the filesystem.
	if err := CheckoutIndex(c, CheckoutIndexOptions{Force: true, UpdateStat: true}, checkoutfiles); err != nil {
		return err
	}
	var origB string
	// Get the original HEAD branchname for the reflog
	//origB = Branch(head).BranchName()
	switch h := head.(type) {
	case RefSpec:
		origB = Branch(h).BranchName()
	default:
		if h, err := head.CommitID(c); err == nil {
			origB = h.String()
		}
	}

	if opts.Branch != "" {
		if err := c.CreateBranch(opts.Branch, cid); err != nil {
			return err
		}
		refmsg := fmt.Sprintf("checkout: moving from %s to %s (dgit)", origB, opts.Branch)
		return SymbolicRefUpdate(c, SymbolicRefOptions{}, "HEAD", RefSpec("refs/heads/"+opts.Branch), refmsg)
	}
	if b, ok := commit.(Branch); ok && !opts.Detach {
		// We're checking out a branch, first read the new tree, and
		// then update the SymbolicRef for HEAD, if that succeeds.
		refmsg := fmt.Sprintf("checkout: moving from %s to %s (dgit)", origB, b.BranchName())
		return SymbolicRefUpdate(c, SymbolicRefOptions{}, "HEAD", RefSpec(b), refmsg)
	}
	refmsg := fmt.Sprintf("checkout: moving from %s to %s (dgit)", origB, cid)
	if err := UpdateRef(c, UpdateRefOptions{NoDeref: true, OldValue: head}, "HEAD", cid, refmsg); err != nil {
		return err
	}
	return nil
}

// Implements "git checkout" subcommand of git for variations:
//     git checkout [-f|--ours|--theirs|-m|--conflict=<style>] [<tree-ish>] [--] <paths>...
//     git checkout [-p|--patch] [<tree-ish>] [--] [<paths>...]
func CheckoutFiles(c *Client, opts CheckoutOptions, tree Treeish, files []File) error {
	// If files were specified, we don't want ReadTree to update the workdir,
	// because we only want to (force) update the specified files.
	//
	// If they weren't, we want to checkout a treeish, so let ReadTree update
	// the workdir so that we don't lose any changes.
	// Load the index so that we can check the skip worktree bit if applicable
	index, err := c.GitDir.ReadIndex()
	if err != nil {
		return err
	}
	imap := index.GetMap()
	expandedfiles, err := LsTree(c, LsTreeOptions{Recurse: true}, tree, files)
	if err != nil {
		return err
	}
	files = make([]File, 0, len(files))
	for _, entry := range expandedfiles {
		f, err := entry.PathName.FilePath(c)
		if err != nil {
			return err
		}
		if opts.IgnoreSkipWorktreeBits {
			files = append(files, f)
			continue
		}
		if entry, ok := imap[entry.PathName]; ok && entry.SkipWorktree() {
			continue
		}
		files = append(files, f)
	}

	// We just want to load the tree as an index so that CheckoutIndexUncommited, so we
	// specify DryRun.
	treeidx, err := ReadTree(c, ReadTreeOptions{DryRun: true}, tree)
	if err != nil {
		return err
	}

	return CheckoutIndexUncommited(c, treeidx, CheckoutIndexOptions{Force: true, UpdateStat: true}, files)
}
