package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// TerminationSubject 标识终止记录所针对的库存对象类型。
type TerminationSubject string

const (
	TerminationOfSpecimen  TerminationSubject = "specimen"
	TerminationOfSubsample TerminationSubject = "subsample"
)

// TerminationRecord 永久保留一次终止决定。终止后的对象不能再新增子样本，
// 已有的父子与去除关系仍然完整可查。
type TerminationRecord struct {
	ID         string             `json:"id"`
	Subject    TerminationSubject `json:"subject"`
	TargetID   string             `json:"target_id"`
	Reason     string             `json:"reason"`
	RecordedBy string             `json:"recorded_by"`
	CreatedAt  time.Time          `json:"created_at"`
}

func NewTerminationRecord(
	id string,
	subject TerminationSubject,
	targetID string,
	reason string,
	recordedBy string,
	now time.Time,
) (TerminationRecord, error) {
	if id == "" {
		return TerminationRecord{}, contracts.Internal("missing_generated_id", "终止记录标识未生成", nil)
	}
	if subject != TerminationOfSpecimen && subject != TerminationOfSubsample {
		return TerminationRecord{}, contracts.Invalid("invalid_termination_subject", "终止对象类型不合法")
	}
	if targetID == "" {
		return TerminationRecord{}, contracts.NotFound("target_not_found", "未找到终止对象")
	}
	reasonValue, err := requiredText("终止原因", reason, 300)
	if err != nil {
		return TerminationRecord{}, err
	}
	recorder, err := requiredText("记录人", recordedBy, 80)
	if err != nil {
		return TerminationRecord{}, err
	}
	return TerminationRecord{
		ID:         id,
		Subject:    subject,
		TargetID:   targetID,
		Reason:     reasonValue,
		RecordedBy: recorder,
		CreatedAt:  now.UTC(),
	}, nil
}
