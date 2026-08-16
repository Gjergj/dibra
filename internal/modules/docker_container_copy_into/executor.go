package docker_container_copy_into

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	posixpath "path"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

const defaultMaxFileSizeForDiff = 104448

type sourceFile struct {
	path       string
	content    []byte
	info       fs.FileInfo
	linkTarget string
	mode       int64
	size       int64
	header     string
	symlink    bool
}

type containerFile struct {
	path       string
	found      bool
	mode       os.FileMode
	size       int64
	linkTarget string
}

type archiveComparison struct {
	header  *tar.Header
	equal   bool
	content []byte
}

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	req, source, maxDiff, err := validateAndPrepareSource(req, dependencies.FileSystem)
	if err != nil {
		return failedResponse(err.Error())
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	ownerID, groupID := 0, 0
	if req.OwnerID == nil {
		user, err := docker.GetContainerUser(ctx, apiClient, req.Container)
		if err != nil {
			return failedResponse(userGroupError(req.Container, err))
		}
		ownerID, groupID, err = docker.GetContainerUserIDs(ctx, apiClient, req.Container, user)
		if err != nil {
			return failedResponse(userGroupError(req.Container, err))
		}
	} else {
		ownerID, groupID = *req.OwnerID, *req.GroupID
	}

	diff := map[string]any(nil)
	if state.DiffMode {
		diff = make(map[string]any)
		if err := addSourceDiff(diff, source, maxDiff, dependencies.FileSystem); err != nil {
			return failedResponse(err.Error())
		}
	}

	containerPath, changed, err := compareDestination(
		ctx, apiClient, req, source, ownerID, groupID, maxDiff, diff, dependencies.FileSystem,
	)
	if err != nil {
		return failedResponse(err.Error())
	}
	if changed && !state.CheckMode {
		if err := copySource(ctx, apiClient, req.Container, containerPath, source, ownerID, groupID, dependencies); err != nil {
			return failedResponse(err.Error())
		}
	}

	response := Response{Changed: changed, ContainerPath: containerPath}
	if len(diff) > 0 {
		if _, ok := diff["before"]; !ok {
			diff["before"] = nil
		}
		if _, ok := diff["after"]; !ok {
			diff["after"] = nil
		}
		response.Diff = diff
	}
	return response
}

func validateAndPrepareSource(req Request, fileSystem docker.FileSystem) (Request, sourceFile, int, error) {
	if req.Container == "" {
		return req, sourceFile{}, 0, errors.New("container is required")
	}
	if req.ContainerPath == "" {
		return req, sourceFile{}, 0, errors.New("container_path is required")
	}
	if (req.Path == nil) == (req.Content == nil) {
		return req, sourceFile{}, 0, errors.New("exactly one of path and content must be supplied")
	}
	if (req.OwnerID == nil) != (req.GroupID == nil) {
		return req, sourceFile{}, 0, errors.New("owner_id and group_id must be supplied together")
	}
	req.ContainerPath = normalizeContainerPath(req.ContainerPath)
	if req.ModeParse == "" {
		req.ModeParse = "legacy"
	}
	mode, supplied, err := parseMode(req.Mode, req.ModeParse)
	if err != nil {
		return req, sourceFile{}, 0, fmt.Errorf("Error while parsing 'mode': %v", err)
	}
	maxDiff := defaultMaxFileSizeForDiff
	if req.MaxFileSizeForDiff != nil {
		maxDiff = *req.MaxFileSizeForDiff
	}

	if req.Content != nil {
		if !supplied {
			return req, sourceFile{}, 0, errors.New("missing parameter(s) required by 'content': mode")
		}
		content := []byte(*req.Content)
		if req.ContentIsB64 {
			content, err = base64.StdEncoding.DecodeString(*req.Content)
			if err != nil {
				return req, sourceFile{}, 0, fmt.Errorf("Cannot Base64 decode the content option: %v", err)
			}
		}
		return req, sourceFile{
			content: content,
			mode:    mode,
			size:    int64(len(content)),
			header:  "dynamically generated",
		}, maxDiff, nil
	}

	localFollow := boolDefault(req.LocalFollow, true)
	var info fs.FileInfo
	if localFollow {
		info, err = fileSystem.Stat(*req.Path)
	} else {
		info, err = fileSystem.Lstat(*req.Path)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return req, sourceFile{}, 0, fmt.Errorf("Cannot find local file %s", *req.Path)
		}
		return req, sourceFile{}, 0, fmt.Errorf("Unexpected error: %v", err)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return req, sourceFile{}, 0, fmt.Errorf("Local path %s is not a symbolic link or file", *req.Path)
	}
	source := sourceFile{
		path:    *req.Path,
		info:    info,
		mode:    fileMode(info.Mode()),
		size:    info.Size(),
		header:  *req.Path,
		symlink: info.Mode()&os.ModeSymlink != 0,
	}
	if supplied {
		source.mode = mode
	}
	if source.symlink {
		source.linkTarget, err = fileSystem.Readlink(*req.Path)
		if err != nil {
			return req, sourceFile{}, 0, fmt.Errorf("Unexpected error: %v", err)
		}
		source.size = int64(len(source.linkTarget))
	}
	return req, source, maxDiff, nil
}

func compareDestination(
	ctx context.Context,
	apiClient client.APIClient,
	req Request,
	source sourceFile,
	ownerID, groupID, maxDiff int,
	diff map[string]any,
	fileSystem docker.FileSystem,
) (string, bool, error) {
	if req.Force != nil && *req.Force && !req.Follow && diff == nil {
		return req.ContainerPath, true, nil
	}

	destination, err := statContainerFile(ctx, apiClient, req.Container, req.ContainerPath, req.Follow)
	if err != nil {
		return req.ContainerPath, false, engineError(req.Container, err)
	}
	containerPath := destination.path
	if !destination.found {
		if diff != nil {
			diff["before_header"] = containerPath
			diff["before"] = ""
		}
		return containerPath, true, nil
	}

	if req.Force != nil && !*req.Force {
		if err := addDestinationDiff(ctx, apiClient, req.Container, destination, maxDiff, diff, nil); err != nil {
			return containerPath, false, err
		}
		copyDestinationDiffToSource(diff)
		return containerPath, false, nil
	}
	if req.Force != nil && *req.Force {
		if err := addDestinationDiff(ctx, apiClient, req.Container, destination, maxDiff, diff, nil); err != nil {
			return containerPath, false, err
		}
		return containerPath, true, nil
	}

	if source.symlink {
		if destination.mode&os.ModeSymlink == 0 {
			if err := addDestinationDiff(ctx, apiClient, req.Container, destination, maxDiff, diff, nil); err != nil {
				return containerPath, false, err
			}
			return containerPath, true, nil
		}
		comparison, err := compareContainerArchive(ctx, apiClient, req.Container, containerPath, nil, false)
		if err != nil {
			return containerPath, false, err
		}
		equal := comparison.header.Typeflag == tar.TypeSymlink &&
			comparison.header.Linkname == source.linkTarget
		addArchiveDestinationDiff(diff, containerPath, comparison, maxDiff)
		return containerPath, !equal, nil
	}

	if !destination.mode.IsRegular() ||
		destination.size != source.size ||
		fileMode(destination.mode) != source.mode {
		if err := addDestinationDiff(ctx, apiClient, req.Container, destination, maxDiff, diff, nil); err != nil {
			return containerPath, false, err
		}
		return containerPath, true, nil
	}

	sourceReader, closeSource, err := openSource(source, fileSystem)
	if err != nil {
		return containerPath, false, err
	}
	defer closeSource()
	comparison, err := compareContainerArchive(
		ctx,
		apiClient,
		req.Container,
		containerPath,
		sourceReader,
		diff != nil && (destination.size <= int64(maxDiff) || maxDiff <= 0),
	)
	if err != nil {
		return containerPath, false, err
	}
	equal := comparison.equal &&
		comparison.header.Typeflag != tar.TypeSymlink &&
		comparison.header.Mode&0xfff == source.mode&0xfff &&
		comparison.header.Uid == ownerID &&
		comparison.header.Gid == groupID &&
		comparison.header.Size == source.size
	addArchiveDestinationDiff(diff, containerPath, comparison, maxDiff)
	return containerPath, !equal, nil
}

func statContainerFile(ctx context.Context, apiClient client.APIClient, containerName, containerPath string, follow bool) (containerFile, error) {
	seen := map[string]bool{}
	current := containerPath
	for {
		if seen[current] {
			return containerFile{}, fmt.Errorf("Found infinite symbolic link loop when trying to stat %q", current)
		}
		seen[current] = true
		result, err := apiClient.ContainerStatPath(ctx, containerName, client.ContainerStatPathOptions{Path: current})
		if err != nil {
			if docker.IsNotFoundError(err) {
				return containerFile{path: current}, nil
			}
			return containerFile{}, err
		}
		entry := containerFile{
			path:       current,
			found:      true,
			mode:       result.Stat.Mode,
			size:       result.Stat.Size,
			linkTarget: result.Stat.LinkTarget,
		}
		if !follow || entry.mode&os.ModeSymlink == 0 {
			return entry, nil
		}
		if posixpath.IsAbs(entry.linkTarget) {
			current = posixpath.Clean(entry.linkTarget)
		} else {
			current = posixpath.Clean(posixpath.Join(posixpath.Dir(current), entry.linkTarget))
		}
	}
}

func addSourceDiff(diff map[string]any, source sourceFile, maxDiff int, fileSystem docker.FileSystem) error {
	if source.size > int64(maxDiff) && maxDiff > 0 {
		diff["src_larger"] = maxDiff
		return nil
	}
	diff["after_header"] = source.header
	if source.symlink {
		diff["after"] = source.linkTarget
		return nil
	}
	content := source.content
	if source.path != "" {
		var err error
		content, err = fileSystem.ReadFile(source.path)
		if err != nil {
			return fmt.Errorf("Unexpected error: %v", err)
		}
	}
	if isBinary(content) {
		diff["src_binary"] = 1
		delete(diff, "after_header")
		return nil
	}
	diff["after"] = string(content)
	return nil
}

func addDestinationDiff(
	ctx context.Context,
	apiClient client.APIClient,
	containerName string,
	destination containerFile,
	maxDiff int,
	diff map[string]any,
	compare io.Reader,
) error {
	if diff == nil {
		return nil
	}
	diff["before_header"] = destination.path
	switch {
	case destination.mode.IsDir():
		diff["before"] = "(directory)"
	case destination.mode&os.ModeSymlink != 0:
		diff["before"] = destination.linkTarget
	case destination.mode&os.ModeNamedPipe != 0:
		diff["before"] = "(named pipe)"
	case destination.mode&os.ModeSocket != 0:
		diff["before"] = "(socket)"
	case destination.mode&os.ModeDevice != 0 && destination.mode&os.ModeCharDevice != 0:
		diff["before"] = "(character device)"
	case destination.mode&os.ModeDevice != 0:
		diff["before"] = "(device)"
	case destination.mode&os.ModeTemporary != 0:
		diff["before"] = "(temporary file)"
	case !destination.mode.IsRegular():
		diff["before"] = "(unknown filesystem object)"
	case destination.size > int64(maxDiff) && maxDiff > 0:
		diff["dst_larger"] = maxDiff
	default:
		comparison, err := compareContainerArchive(ctx, apiClient, containerName, destination.path, compare, true)
		if err != nil {
			return err
		}
		addArchiveDestinationDiff(diff, destination.path, comparison, maxDiff)
	}
	return nil
}

func addArchiveDestinationDiff(diff map[string]any, containerPath string, comparison archiveComparison, maxDiff int) {
	if diff == nil || comparison.header == nil {
		return
	}
	diff["before_header"] = containerPath
	switch comparison.header.Typeflag {
	case tar.TypeSymlink, tar.TypeLink:
		diff["before"] = comparison.header.Linkname
	case tar.TypeDir:
		diff["before"] = "(directory)"
	default:
		if comparison.header.Size > int64(maxDiff) && maxDiff > 0 {
			diff["dst_larger"] = maxDiff
		} else if isBinary(comparison.content) {
			diff["dst_binary"] = 1
			delete(diff, "before_header")
		} else {
			diff["before"] = string(comparison.content)
		}
	}
}

func compareContainerArchive(
	ctx context.Context,
	apiClient client.APIClient,
	containerName, containerPath string,
	source io.Reader,
	capture bool,
) (archiveComparison, error) {
	result, err := apiClient.CopyFromContainer(ctx, containerName, client.CopyFromContainerOptions{SourcePath: containerPath})
	if err != nil {
		return archiveComparison{}, engineError(containerName, err)
	}
	defer result.Content.Close()
	reader := tar.NewReader(result.Content)
	header, err := reader.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return archiveComparison{}, errors.New("Unexpected error: received tarfile is empty")
		}
		return archiveComparison{}, fmt.Errorf("Unexpected error reading container archive: %v", err)
	}
	headerCopy := *header
	comparison := archiveComparison{header: &headerCopy, equal: source == nil}
	if header.Typeflag == tar.TypeReg || header.Typeflag == 0 {
		var destination io.Reader = reader
		var captured bytes.Buffer
		if capture {
			destination = io.TeeReader(reader, &captured)
		}
		if source != nil {
			comparison.equal, err = readersEqual(destination, source)
		} else {
			_, err = io.Copy(io.Discard, destination)
		}
		comparison.content = captured.Bytes()
		if err != nil {
			return archiveComparison{}, fmt.Errorf("Unexpected error reading container archive: %v", err)
		}
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		if err == nil {
			return archiveComparison{}, errors.New("Unexpected error: received tarfile contains more than one file")
		}
		return archiveComparison{}, fmt.Errorf("Unexpected error reading container archive: %v", err)
	}
	return comparison, nil
}

func readersEqual(first, second io.Reader) (bool, error) {
	firstBuffer := make([]byte, 64*1024)
	secondBuffer := make([]byte, 64*1024)
	for {
		firstN, firstErr := io.ReadFull(first, firstBuffer)
		secondN, secondErr := io.ReadFull(second, secondBuffer)
		if firstN != secondN || !bytes.Equal(firstBuffer[:firstN], secondBuffer[:secondN]) {
			return false, nil
		}
		firstDone := errors.Is(firstErr, io.EOF) || errors.Is(firstErr, io.ErrUnexpectedEOF)
		secondDone := errors.Is(secondErr, io.EOF) || errors.Is(secondErr, io.ErrUnexpectedEOF)
		if firstDone || secondDone {
			return firstDone && secondDone, nil
		}
		if firstErr != nil {
			return false, firstErr
		}
		if secondErr != nil {
			return false, secondErr
		}
	}
}

func copySource(
	ctx context.Context,
	apiClient client.APIClient,
	containerName, containerPath string,
	source sourceFile,
	ownerID, groupID int,
	dependencies docker.Dependencies,
) error {
	archiveReader, archiveWriter := io.Pipe()
	writeResult := make(chan error, 1)
	go func() {
		writeResult <- writeSourceArchive(archiveWriter, containerPath, source, ownerID, groupID, dependencies)
	}()
	_, copyErr := apiClient.CopyToContainer(ctx, containerName, client.CopyToContainerOptions{
		DestinationPath:           posixpath.Dir(containerPath),
		Content:                   archiveReader,
		AllowOverwriteDirWithFile: true,
	})
	_ = archiveReader.CloseWithError(copyErr)
	writeErr := <-writeResult
	if copyErr != nil {
		return engineError(containerName, copyErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return nil
}

func writeSourceArchive(
	writer *io.PipeWriter,
	containerPath string,
	source sourceFile,
	ownerID, groupID int,
	dependencies docker.Dependencies,
) (returnErr error) {
	defer func() {
		_ = writer.CloseWithError(returnErr)
	}()
	tarWriter := tar.NewWriter(writer)
	defer func() {
		if err := tarWriter.Close(); returnErr == nil && err != nil {
			returnErr = fmt.Errorf("failed to close tar writer: %v", err)
		}
	}()
	modTime := dependencies.Clock.Now()
	if source.info != nil {
		modTime = source.info.ModTime()
	}
	header := &tar.Header{
		Name:     posixpath.Base(containerPath),
		Mode:     source.mode,
		Uid:      ownerID,
		Gid:      groupID,
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
		Size:     source.size,
	}
	if source.symlink {
		header.Typeflag = tar.TypeSymlink
		header.Linkname = source.linkTarget
		header.Size = 0
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %v", err)
	}
	if source.symlink {
		return nil
	}
	reader, closeReader, err := openSource(source, dependencies.FileSystem)
	if err != nil {
		return err
	}
	defer closeReader()
	written, err := io.CopyN(tarWriter, reader, source.size)
	if err != nil {
		return fmt.Errorf("failed to write tar content after %d bytes: %v", written, err)
	}
	return nil
}

func openSource(source sourceFile, fileSystem docker.FileSystem) (io.Reader, func(), error) {
	if source.path == "" {
		return bytes.NewReader(source.content), func() {}, nil
	}
	file, err := fileSystem.Open(source.path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("Unexpected error: %v", err)
	}
	return file, func() { _ = file.Close() }, nil
}

func parseMode(raw json.RawMessage, strategy string) (int64, bool, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, false, err
	}
	var parsed *big.Int
	var err error
	switch strategy {
	case "legacy":
		parsed, err = parseLegacy(value)
	case "modern":
		parsed, err = parseModern(value)
	case "octal_string_only":
		parsed, err = parseOctalStringOnly(value)
	default:
		return 0, false, fmt.Errorf("must be one of legacy, modern, octal_string_only; got %q", strategy)
	}
	if err != nil {
		return 0, false, err
	}
	if parsed.Sign() < 0 {
		return 0, false, fmt.Errorf("'mode' must not be negative; got %s", parsed.String())
	}
	if !parsed.IsInt64() {
		return 0, false, fmt.Errorf("'mode' is out of range; got %s", parsed.String())
	}
	return parsed.Int64(), true, nil
}

func parseLegacy(value any) (*big.Int, error) {
	switch typed := value.(type) {
	case string:
		return parseBigInt(strings.TrimSpace(typed), 10)
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			return nil, fmt.Errorf("must be an integer, got %s", typed)
		}
		return parseBigInt(typed.String(), 10)
	default:
		return nil, fmt.Errorf("must be an integer, got %v", value)
	}
}

func parseModern(value any) (*big.Int, error) {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		sign := ""
		if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
			sign, text = text[:1], text[1:]
		}
		text = strings.TrimPrefix(strings.TrimPrefix(text, "0o"), "0O")
		return parseBigInt(sign+text, 8)
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			return nil, fmt.Errorf("must be an octal string or an integer, got %s", typed)
		}
		return parseBigInt(typed.String(), 10)
	case int:
		return big.NewInt(int64(typed)), nil
	case int64:
		return big.NewInt(typed), nil
	case *big.Int:
		return new(big.Int).Set(typed), nil
	default:
		return nil, fmt.Errorf("must be an octal string or an integer, got %v", value)
	}
}

func parseOctalStringOnly(value any) (*big.Int, error) {
	typed, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("must be an octal string, got %v", value)
	}
	text := strings.TrimSpace(typed)
	sign := ""
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		sign, text = text[:1], text[1:]
	}
	text = strings.TrimPrefix(strings.TrimPrefix(text, "0o"), "0O")
	return parseBigInt(sign+text, 8)
}

func parseBigInt(value string, base int) (*big.Int, error) {
	result, ok := new(big.Int).SetString(value, base)
	if !ok {
		return nil, fmt.Errorf("invalid value %q", value)
	}
	return result, nil
}

func normalizeContainerPath(value string) string {
	if !posixpath.IsAbs(value) {
		value = "/" + value
	}
	return posixpath.Clean(value)
}

func fileMode(mode os.FileMode) int64 {
	value := int64(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		value |= 0o4000
	}
	if mode&os.ModeSetgid != 0 {
		value |= 0o2000
	}
	if mode&os.ModeSticky != 0 {
		value |= 0o1000
	}
	return value
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func isBinary(content []byte) bool {
	return bytes.IndexByte(content, 0) >= 0
}

func copyDestinationDiffToSource(diff map[string]any) {
	if diff == nil {
		return
	}
	for _, pair := range [][2]string{
		{"dst_larger", "src_larger"},
		{"dst_binary", "src_binary"},
		{"before_header", "after_header"},
		{"before", "after"},
	} {
		if value, ok := diff[pair[0]]; ok {
			diff[pair[1]] = value
		} else {
			delete(diff, pair[1])
		}
	}
}

func engineError(containerName string, err error) error {
	if docker.IsNotFoundError(err) {
		return fmt.Errorf("Could not find container %q or resource in it (%v)", containerName, err)
	}
	return fmt.Errorf("An unexpected Docker error occurred for container %q: %v", containerName, err)
}

func userGroupError(containerName string, err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not running") || strings.Contains(message, "paused") || strings.Contains(message, "409") {
		return fmt.Sprintf("Cannot execute command in paused container %q", containerName)
	}
	return fmt.Sprintf("Unexpected error determining user and group ID for container %q: %v", containerName, err)
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
