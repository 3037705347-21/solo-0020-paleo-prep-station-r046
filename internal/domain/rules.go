package domain

import (
	"regexp"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
)

var catalogPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

const (
	// defaultStockQuantity is applied when a specimen is registered without
	// an explicit initial inventory.
	defaultStockQuantity = 100
	// maxStockQuantity bounds both specimen inventories and a single removal.
	maxStockQuantity = 1_000_000
)

func requiredText(field, value string, max int) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", contracts.Invalid("required_field", field+"不能为空")
	}
	if len([]rune(cleaned)) > max {
		return "", contracts.Invalid("field_too_long", field+"长度超出限制")
	}
	return cleaned, nil
}

func optionalText(field, value string, max int) (string, error) {
	cleaned := strings.TrimSpace(value)
	if len([]rune(cleaned)) > max {
		return "", contracts.Invalid("field_too_long", field+"长度超出限制")
	}
	return cleaned, nil
}

func validateCatalogNumber(value string) (string, error) {
	cleaned, err := requiredText("目录号", value, 64)
	if err != nil {
		return "", err
	}
	if !catalogPattern.MatchString(cleaned) {
		return "", contracts.Invalid(
			"invalid_catalog_number",
			"目录号只能包含字母、数字、点、下划线和连字符",
		)
	}
	return strings.ToUpper(cleaned), nil
}

func validatePercent(value int) error {
	if value < 0 || value > 100 {
		return contracts.Invalid("invalid_percent", "进度比例必须位于 0 到 100 之间")
	}
	return nil
}

// normalizeInitialQuantity treats zero as "unspecified" and applies the
// default inventory; negative or oversized values are rejected.
func normalizeInitialQuantity(value int) (int, error) {
	if value == 0 {
		return defaultStockQuantity, nil
	}
	if value < 0 || value > maxStockQuantity {
		return 0, contracts.Invalid("invalid_quantity", "初始库存必须位于 1 到 1000000 之间")
	}
	return value, nil
}

// validateRemovalQuantity ensures a single sampling removes at least one unit
// and never exceeds the inventory bound.
func validateRemovalQuantity(value int) error {
	if value <= 0 {
		return contracts.Invalid("invalid_quantity", "去除数量必须大于零")
	}
	if value > maxStockQuantity {
		return contracts.Invalid("invalid_quantity", "去除数量超出上限")
	}
	return nil
}

// validateSubsampleCode enforces the same stable identity format as catalog
// numbers and normalizes the code to upper case.
func validateSubsampleCode(value string) (string, error) {
	cleaned, err := requiredText("子样本编码", value, 64)
	if err != nil {
		return "", err
	}
	if !catalogPattern.MatchString(cleaned) {
		return "", contracts.Invalid(
			"invalid_subsample_code",
			"子样本编码只能包含字母、数字、点、下划线和连字符",
		)
	}
	return strings.ToUpper(cleaned), nil
}
