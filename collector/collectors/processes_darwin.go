package collectors

import (
	"fmt"
	"mosun_collector/collector/conf"
)

func AddProcessConfig(params conf.ProcessParams) error {
	return fmt.Errorf("process watching not implemented on Darwin")
}

func WatchProcesses() {
}
