package template

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aisbergg/gonja/pkg/gonja"
	"github.com/aisbergg/gonja/pkg/gonja/exec"
	"github.com/aisbergg/gonja/pkg/gonja/loaders"
)

type Options struct {
	NewlineSequence     string
	VariableStartString string
	VariableEndString   string
	BlockStartString    string
	BlockEndString      string
	CommentStartString  string
	CommentEndString    string
	TrimBlocks          *bool
	LstripBlocks        *bool
}

type Metadata struct {
	HostName string
	DestPath string
}

func RenderFile(srcPath string, context map[string]interface{}, meta Metadata, opts Options) (string, map[string]interface{}, error) {
	info, err := osStat(srcPath)
	if err != nil {
		return "", nil, err
	}

	ctx := make(map[string]interface{})
	for k, v := range context {
		ctx[k] = v
	}

	templateDestPath := meta.DestPath
	ctx["template_path"] = srcPath
	ctx["template_fullpath"] = srcPath
	ctx["template_host"] = meta.HostName
	ctx["template_uid"] = info.uid
	ctx["template_destpath"] = templateDestPath
	ctx["template_run_date"] = time.Now().Format(time.RFC3339)
	ctx["ansible_managed"] = "Managed by dibra (template=" + filepath.Base(srcPath) + ")"
	ctx["template_dest"] = templateDestPath
	ctx["template"] = ctx

	envOptions := []gonja.Option{
		gonja.OptLoader(loaders.MustNewFileSystemLoader(filepath.Dir(srcPath))),
		gonja.OptUndefined(exec.NewChainedStrictUndefinedValue),
	}

	if opts.NewlineSequence != "" {
		envOptions = append(envOptions, gonja.OptNewlineSequence(opts.NewlineSequence))
	}
	if opts.VariableStartString != "" {
		envOptions = append(envOptions, gonja.OptVariableStartString(opts.VariableStartString))
	}
	if opts.VariableEndString != "" {
		envOptions = append(envOptions, gonja.OptVariableEndString(opts.VariableEndString))
	}
	if opts.BlockStartString != "" {
		envOptions = append(envOptions, gonja.OptBlockStartString(opts.BlockStartString))
	}
	if opts.BlockEndString != "" {
		envOptions = append(envOptions, gonja.OptBlockEndString(opts.BlockEndString))
	}
	if opts.CommentStartString != "" {
		envOptions = append(envOptions, gonja.OptCommentStartString(opts.CommentStartString))
	}
	if opts.CommentEndString != "" {
		envOptions = append(envOptions, gonja.OptCommentEndString(opts.CommentEndString))
	}
	if opts.TrimBlocks != nil {
		if *opts.TrimBlocks {
			envOptions = append(envOptions, gonja.OptTrimBlocks())
		} else {
			envOptions = append(envOptions, gonja.OptNoTrimBlocks())
		}
	} else {
		envOptions = append(envOptions, gonja.OptTrimBlocks())
	}
	if opts.LstripBlocks != nil {
		if *opts.LstripBlocks {
			envOptions = append(envOptions, gonja.OptLstripBlocks())
		} else {
			envOptions = append(envOptions, gonja.OptNoLstripBlocks())
		}
	}

	env := gonja.NewEnvironment(envOptions...)
	registerAnsibleFilters(env)

	templateRel, err := filepath.Rel(filepath.Dir(srcPath), srcPath)
	if err != nil {
		templateRel = filepath.Base(srcPath)
	}

	tpl, err := env.FromFile(templateRel)
	if err != nil {
		return "", nil, err
	}

	rendered, err := tpl.Execute(ctx)
	if err != nil {
		return "", nil, err
	}

	return rendered, ctx, nil
}

type fileInfo struct {
	uid int
}

func osStat(path string) (fileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileInfo{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileInfo{}, fmt.Errorf("failed to read file metadata")
	}
	return fileInfo{uid: int(stat.Uid)}, nil
}
