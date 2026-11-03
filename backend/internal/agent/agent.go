package agent

func Run(task string) error {
	return RunPath(task, "")
}
