// Copied from: https://github.com/docker/cli/blob/master/cli/command/container/cp.go

package ch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/go-units"
	"github.com/morikuni/aec"
)

type cpConfig struct {
	followLink bool
	copyUIDGID bool
	quiet      bool
	sourcePath string
	destPath   string
	container  string
}

type copyOptions struct {
	source      string
	destination string
	followLink  bool
	copyUIDGID  bool
	quiet       bool
}

type copyDirection int

// copyProgressPrinter wraps io.ReadCloser to print progress information when
// copying files to/from a container.
type copyProgressPrinter struct {
	io.ReadCloser
	toContainer bool
	total       *float64
	writer      io.Writer
}

func (pt *copyProgressPrinter) Read(p []byte) (int, error) {
	n, err := pt.ReadCloser.Read(p)
	*pt.total += float64(n)

	if err == nil {
		_, _ = fmt.Fprint(pt.writer, aec.Restore)
		_, _ = fmt.Fprint(pt.writer, aec.EraseLine(aec.EraseModes.All))
		if pt.toContainer {
			_, _ = fmt.Fprintln(pt.writer, "Copying to container - "+units.HumanSize(*pt.total))
		} else {
			_, _ = fmt.Fprintln(pt.writer, "Copying from container - "+units.HumanSize(*pt.total))
		}
	}

	return n, err
}

const (
	fromContainer copyDirection = 1 << iota
	toContainer
	acrossContainers = fromContainer | toContainer
)

// We use `:` as a delimiter between CONTAINER and PATH, but `:` could also be
// in a valid LOCALPATH, like `file:name.txt`. We can resolve this ambiguity by
// requiring a LOCALPATH with a `:` to be made explicit with a relative or
// absolute path:
// 	`/path/to/file:name.txt` or `./file:name.txt`
//
// This is apparently how `scp` handles this as well:
// 	http://www.cyberciti.biz/faq/rsync-scp-file-name-with-colon-punctuation-in-it/
//
// We can't simply check for a filepath separator because container names may
// have a separator, e.g., "host0/cname1" if container is in a Docker cluster,
// so we have to check for a `/` or `.` prefix. Also, in the case of a Windows
// client, a `:` could be part of an absolute Windows path, in which case it
// is immediately proceeded by a backslash.
func splitCpArg(arg string) (container, path string) {
	if system.IsAbs(arg) {
		// Explicit local absolute path, e.g., `C:\foo` or `/foo`.
		return "", arg
	}

	parts := strings.SplitN(arg, ":", 2)

	if len(parts) == 1 || strings.HasPrefix(parts[0], ".") {
		// Either there's no `:` in the arg
		// OR it's an explicit local relative path like `./file:name.txt`.
		return "", arg
	}

	return parts[0], parts[1]
}

func runCopy(client *docker.Client, opts copyOptions) error {
	srcContainer, srcPath := splitCpArg(opts.source)
	destContainer, destPath := splitCpArg(opts.destination)

	copyConfig := cpConfig{
		followLink: opts.followLink,
		copyUIDGID: opts.copyUIDGID,
		quiet:      opts.quiet,
		sourcePath: srcPath,
		destPath:   destPath,
	}

	var direction copyDirection
	if srcContainer != "" {
		direction |= fromContainer
		copyConfig.container = srcContainer
	}
	if destContainer != "" {
		direction |= toContainer
		copyConfig.container = destContainer
	}

	ctx := context.Background()

	switch direction {
	case fromContainer:
		return copyFromContainer(ctx, client, copyConfig)
	case toContainer:
		return copyToContainer(ctx, client, copyConfig)
	case acrossContainers:
		return errors.New("copying between containers is not supported")
	default:
		return errors.New("must specify at least one container source")
	}
}

func copyFromContainer(ctx context.Context, client *docker.Client, copyConfig cpConfig) (err error) {
	dstPath := copyConfig.destPath
	srcPath := copyConfig.sourcePath

	if dstPath != "-" {
		// Get an absolute destination path.
		dstPath, err = resolveLocalPath(dstPath)
		if err != nil {
			return err
		}
	}

	if err := command.ValidateOutputPath(dstPath); err != nil {
		return err
	}

	// if client requests to follow symbol link, then must decide target file to be copied
	var rebaseName string
	if copyConfig.followLink {
		srcStat, err := client.ContainerStatPath(ctx, copyConfig.container, srcPath)

		// If the destination is a symbolic link, we should follow it.
		if err == nil && srcStat.Mode&os.ModeSymlink != 0 {
			linkTarget := srcStat.LinkTarget
			if !system.IsAbs(linkTarget) {
				// Join with the parent directory.
				srcParent, _ := archive.SplitPathDirEntry(srcPath)
				linkTarget = filepath.Join(srcParent, linkTarget)
			}

			linkTarget, rebaseName = archive.GetRebaseName(srcPath, linkTarget)
			srcPath = linkTarget
		}

	}

	content, stat, err := client.CopyFromContainer(ctx, copyConfig.container, srcPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = content.Close()
	}()

	if dstPath == "-" {
		_, err = io.Copy(os.Stdout, content)
		return err
	}

	srcInfo := archive.CopyInfo{
		Path:       srcPath,
		Exists:     true,
		IsDir:      stat.Mode.IsDir(),
		RebaseName: rebaseName,
	}

	var copiedSize float64
	if !copyConfig.quiet {
		content = &copyProgressPrinter{
			ReadCloser:  content,
			toContainer: false,
			writer:      os.Stderr,
			total:       &copiedSize,
		}
	}

	preArchive := content
	if len(srcInfo.RebaseName) != 0 {
		_, srcBase := archive.SplitPathDirEntry(srcInfo.Path)
		preArchive = archive.RebaseArchiveEntries(content, srcBase, srcInfo.RebaseName)
	}

	if copyConfig.quiet {
		return archive.CopyTo(preArchive, srcInfo, dstPath)
	}

	_, _ = fmt.Fprint(os.Stderr, aec.Save)
	_, _ = fmt.Fprintln(os.Stderr, "Preparing to copy...")
	res := archive.CopyTo(preArchive, srcInfo, dstPath)
	_, _ = fmt.Fprint(os.Stderr, aec.Restore)
	_, _ = fmt.Fprint(os.Stderr, aec.EraseLine(aec.EraseModes.All))
	_, _ = fmt.Fprintln(os.Stderr, "Successfully copied", units.HumanSize(copiedSize), "to", dstPath)

	return res
}

func resolveLocalPath(localPath string) (absPath string, err error) {
	if absPath, err = filepath.Abs(localPath); err != nil {
		return
	}
	return archive.PreserveTrailingDotOrSeparator(absPath, localPath, filepath.Separator), nil
}

// In order to get the copy behavior right, we need to know information
// about both the source and destination. The API is a simple tar
// archive/extract API but we can use the stat info header about the
// destination to be more informed about exactly what the destination is.
func copyToContainer(ctx context.Context, client *docker.Client, copyConfig cpConfig) (err error) {
	srcPath := copyConfig.sourcePath
	dstPath := copyConfig.destPath

	if srcPath != "-" {
		// Get an absolute source path.
		srcPath, err = resolveLocalPath(srcPath)
		if err != nil {
			return err
		}
	}

	// Prepare destination copy info by stat-ing the container path.
	dstInfo := archive.CopyInfo{Path: dstPath}
	dstStat, err := client.ContainerStatPath(ctx, copyConfig.container, dstPath)

	// If the destination is a symbolic link, we should evaluate it.
	if err == nil && dstStat.Mode&os.ModeSymlink != 0 {
		linkTarget := dstStat.LinkTarget
		if !system.IsAbs(linkTarget) {
			// Join with the parent directory.
			dstParent, _ := archive.SplitPathDirEntry(dstPath)
			linkTarget = filepath.Join(dstParent, linkTarget)
		}

		dstInfo.Path = linkTarget
		dstStat, err = client.ContainerStatPath(ctx, copyConfig.container, linkTarget)
	}

	// Validate the destination path
	if err := command.ValidateOutputPathFileMode(dstStat.Mode); err != nil {
		return fmt.Errorf(`destination "%s:%s" must be a directory or a regular file: %w`, copyConfig.container, dstPath, err)
	}

	// Ignore any error and assume that the parent directory of the destination
	// path exists, in which case the copy may still succeed. If there is any
	// type of conflict (e.g., non-directory overwriting an existing directory
	// or vice versa) the extraction will fail. If the destination simply did
	// not exist, but the parent directory does, the extraction will still
	// succeed.
	if err == nil {
		dstInfo.Exists, dstInfo.IsDir = true, dstStat.Mode.IsDir()
	}

	var (
		content         io.ReadCloser
		resolvedDstPath string
		copiedSize      float64
	)

	if srcPath == "-" {
		content = os.Stdin
		resolvedDstPath = dstInfo.Path
		if !dstInfo.IsDir {
			return fmt.Errorf("destination \"%s:%s\" must be a directory", copyConfig.container, dstPath)
		}
	} else {
		// Prepare source copy info.
		srcInfo, err := archive.CopyInfoSourcePath(srcPath, copyConfig.followLink)
		if err != nil {
			return err
		}

		srcArchive, err := archive.TarResource(srcInfo)
		if err != nil {
			return err
		}
		defer func() {
			_ = srcArchive.Close()
		}()

		// With the stat info about the local source as well as the
		// destination, we have enough information to know whether we need to
		// alter the archive that we upload so that when the server extracts
		// it to the specified directory in the container we get the desired
		// copy behavior.

		// See comments in the implementation of `archive.PrepareArchiveCopy`
		// for exactly what goes into deciding how and whether the source
		// archive needs to be altered for the correct copy behavior when it is
		// extracted. This function also infers from the source and destination
		// info which directory to extract to, which may be the parent of the
		// destination that the user specified.
		dstDir, preparedArchive, err := archive.PrepareArchiveCopy(srcArchive, srcInfo, dstInfo)
		if err != nil {
			return err
		}
		defer func() {
			_ = preparedArchive.Close()
		}()

		resolvedDstPath = dstDir
		content = preparedArchive
		if !copyConfig.quiet {
			content = &copyProgressPrinter{
				ReadCloser:  content,
				toContainer: true,
				writer:      os.Stderr,
				total:       &copiedSize,
			}
		}
	}

	options := types.CopyToContainerOptions{
		AllowOverwriteDirWithFile: false,
		CopyUIDGID:                copyConfig.copyUIDGID,
	}

	if copyConfig.quiet {
		return client.CopyToContainer(ctx, copyConfig.container, resolvedDstPath, content, options)
	}

	_, _ = fmt.Fprint(os.Stderr, aec.Save)
	_, _ = fmt.Fprintln(os.Stderr, "Preparing to copy...")
	res := client.CopyToContainer(ctx, copyConfig.container, resolvedDstPath, content, options)
	_, _ = fmt.Fprint(os.Stderr, aec.Restore)
	_, _ = fmt.Fprint(os.Stderr, aec.EraseLine(aec.EraseModes.All))
	_, _ = fmt.Fprintln(os.Stderr, "Successfully copied", units.HumanSize(copiedSize), "to", copyConfig.container+":"+dstInfo.Path)

	return res
}
