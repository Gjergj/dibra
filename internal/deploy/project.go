package deploy

import (
	"fmt"
	"path/filepath"

	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/vars"
)

// validateRebootPlacement catches every statically expandable invalid reboot
// before the first playbook is allowed to change the host. Dynamic includes
// are additionally protected by the runner's runtime final-task guard.
func validateRebootPlacement(project Project) error {
	rebootCount := 0
	for playbookIndex, playbook := range project.Manifest.Playbooks {
		playbookPath := filepath.Join(project.Root, filepath.FromSlash(playbook))
		cfg, err := config.Load(playbookPath)
		if err != nil {
			return fmt.Errorf("load playbook %q for preflight: %w", playbook, err)
		}
		baseDir := filepath.Dir(playbookPath)
		playVars := cfg.Vars
		if len(cfg.VarsFiles) > 0 {
			fromFiles, varsErr := vars.LoadVarsFiles(baseDir, cfg.VarsFiles, vars.MergeStrategy(cfg.VarsMerge))
			if varsErr != nil {
				return fmt.Errorf("load vars_files for playbook %q: %w", playbook, varsErr)
			}
			playVars = vars.MergeMaps(playVars, fromFiles, vars.MergeStrategy(cfg.VarsMerge))
		}
		renderPath := func(value string) (string, error) {
			return vars.RenderString(value, playVars)
		}
		tasks, expandErr := config.ExpandImportTasks(cfg.Tasks, baseDir, renderPath)
		if expandErr != nil {
			return fmt.Errorf("expand import_tasks for playbook %q: %w", playbook, expandErr)
		}
		for taskIndex, task := range tasks {
			if task.Reboot == nil {
				continue
			}
			rebootCount++
			if hasTaskLoop(task) {
				return fmt.Errorf("local reboot task %q cannot use a loop", task.Name)
			}
			finalPlaybook := playbookIndex == len(project.Manifest.Playbooks)-1
			finalTask := taskIndex == len(tasks)-1
			if !finalPlaybook || !finalTask {
				return fmt.Errorf("local reboot task %q must be the final task of the final playbook", task.Name)
			}
		}
	}
	if rebootCount > 1 {
		return fmt.Errorf("deployment may contain at most one local reboot task")
	}
	return nil
}

func hasTaskLoop(task config.Task) bool {
	return task.Loop != nil || task.WithItems != nil || task.WithList != nil || task.WithDict != nil || task.WithSequence != nil
}
