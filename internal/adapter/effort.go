package adapter

import (
	"fmt"

	"claude2api/internal/service"
)

type effortOptions struct {
	Effort *string `json:"effort"`
}

/** resolveEffort compares normalized aliases so Messages max and xhigh agree. */
func resolveEffort(field string, native, direct *string, allowMax bool) (string, error) {
	normalize := func(name string, effort *string) (*string, error) {
		if effort == nil {
			return nil, nil
		}
		value := *effort
		if allowMax && value == "max" {
			value = "xhigh"
		}
		switch value {
		case "low", "medium", "high", "xhigh":
			return &value, nil
		default:
			allowed := "low, medium, high, xhigh"
			if allowMax {
				allowed += ", max (映射为 xhigh)"
			}
			return nil, fmt.Errorf("%s 的值 %q 无效，仅支持 %s", name, *effort, allowed)
		}
	}
	native, err := normalize(field, native)
	if err != nil {
		return "", err
	}
	direct, err = normalize("effort", direct)
	if err != nil {
		return "", err
	}
	if native != nil && direct != nil && *native != *direct {
		return "", fmt.Errorf("%s 与 effort 的值冲突", field)
	}
	if native != nil {
		return *native, nil
	}
	if direct != nil {
		return *direct, nil
	}
	return service.DefaultEffort, nil
}
