package domain

import (
	"regexp"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
)

var catalogPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

// subsampleCodePattern 与目录号采用同一套字符集，保证子样本编号可拼接、可检索。
var subsampleCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

// requestKeyPattern 约束客户端幂等键，避免空白和超长键污染去除记录。
var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)

// 最大登记/去除数量，防止异常大值导致库存字段溢出。
const maxQuantity = 1_000_000_000

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

func validatePositiveInt(field string, value int) (int, error) {
	if value <= 0 {
		return 0, contracts.Invalid("invalid_quantity", field+"必须为正整数")
	}
	if value > maxQuantity {
		return 0, contracts.Invalid("invalid_quantity", field+"超出允许范围")
	}
	return value, nil
}

func validateSubsampleCode(value string) (string, error) {
	cleaned, err := requiredText("子样本编号", value, 64)
	if err != nil {
		return "", err
	}
	if !subsampleCodePattern.MatchString(cleaned) {
		return "", contracts.Invalid(
			"invalid_subsample_code",
			"子样本编号只能包含字母、数字、点、下划线和连字符",
		)
	}
	return strings.ToUpper(cleaned), nil
}

// ValidateIdempotencyKey 对可选幂等键做格式约束；空串视为未提供。
func ValidateIdempotencyKey(value string) (string, error) {
	return validateIdempotencyKey(value)
}

// validateIdempotencyKey 对可选幂等键做格式约束；空串视为未提供。
func validateIdempotencyKey(value string) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", nil
	}
	if len([]rune(cleaned)) > 80 || !requestKeyPattern.MatchString(cleaned) {
		return "", contracts.Invalid(
			"invalid_idempotency_key",
			"幂等键只能包含字母、数字、点、下划线、冒号和连字符",
		)
	}
	return cleaned, nil
}
