package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masapico/minihub/internal/domain"
)

const (
	MaxAttachmentFileBytes  = 20 * 1024 * 1024 // 20 MiB
	MaxAttachmentTotalBytes = 50 * 1024 * 1024 // 50 MiB
	MaxAttachmentCount      = 5
)

var allowedAttachmentExtensions = map[string]bool{
	".xlsx": true,
	".xls":  true,
	".docx": true,
	".doc":  true,
	".pptx": true,
	".ppt":  true,
	".pdf":  true,
	".txt":  true,
	".csv":  true,
	".tsv":  true,
	".json": true,
	".md":   true,
}

type StagedAttachment struct {
	ID        string
	ChannelID string
	UserID    string
	Filename  string
	Size      int64
	Data      []byte
	CreatedAt time.Time
}

type AttachmentSummary struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type AIAttachmentPayload struct {
	Name   string `json:"name"`
	Base64 string `json:"data"`
}

type AttachmentManager struct {
	mu     sync.Mutex
	staged map[string]StagedAttachment
}

func newAttachmentManager() *AttachmentManager {
	return &AttachmentManager{
		staged: make(map[string]StagedAttachment),
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" || name == "\\" {
		return "attachment.bin"
	}
	// Replace control characters
	var sb strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 || r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			sb.WriteRune('_')
		} else {
			sb.WriteRune(r)
		}
	}
	result := sb.String()
	if len(result) > 200 {
		ext := filepath.Ext(result)
		if len(ext) < 20 {
			result = result[:200-len(ext)] + ext
		} else {
			result = result[:200]
		}
	}
	return result
}

// StageAttachment validates and temporarily stores an uploaded attachment file in memory.
func (s *Service) StageAttachment(ctx context.Context, userID, channelID, filename string, r io.Reader) (AttachmentSummary, error) {
	if err := s.canPost(ctx, userID, channelID); err != nil {
		return AttachmentSummary{}, err
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedAttachmentExtensions[ext] {
		return AttachmentSummary{}, fmt.Errorf("%w: 対応していないファイル形式です（Excel, Word, PowerPoint, PDF, テキスト/データ形式のみ添付可能）", ErrInvalid)
	}

	sanitized := sanitizeFilename(filename)
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return AttachmentSummary{}, fmt.Errorf("generate attachment id: %w", err)
	}
	id := hex.EncodeToString(randomBytes[:])

	// Read limited content into memory
	limited := io.LimitReader(r, MaxAttachmentFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return AttachmentSummary{}, fmt.Errorf("read attachment file: %w", err)
	}
	if len(data) > MaxAttachmentFileBytes {
		return AttachmentSummary{}, fmt.Errorf("%w: ファイルサイズが上限（20 MiB）を超えています", ErrInvalid)
	}
	if len(data) == 0 {
		return AttachmentSummary{}, fmt.Errorf("%w: ファイルが空です", ErrInvalid)
	}

	now := time.Now()
	s.attachmentMgr.mu.Lock()
	defer s.attachmentMgr.mu.Unlock()

	// Prune staged attachments older than 2 hours
	cutoff := now.Add(-2 * time.Hour)
	for k, item := range s.attachmentMgr.staged {
		if item.CreatedAt.Before(cutoff) {
			delete(s.attachmentMgr.staged, k)
		}
	}

	s.attachmentMgr.staged[id] = StagedAttachment{
		ID:        id,
		ChannelID: channelID,
		UserID:    userID,
		Filename:  sanitized,
		Size:      int64(len(data)),
		Data:      data,
		CreatedAt: now,
	}

	return AttachmentSummary{
		ID:       id,
		Filename: sanitized,
		Size:     int64(len(data)),
	}, nil
}

// consumeStagedAttachments claims staged attachments and checks totals.
func (s *Service) consumeStagedAttachments(userID, channelID string, ids []string) ([]domain.Attachment, []AIAttachmentPayload, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if len(ids) > MaxAttachmentCount {
		return nil, nil, fmt.Errorf("%w: 添付ファイルは1回の投稿につき%d件までです", ErrInvalid, MaxAttachmentCount)
	}

	s.attachmentMgr.mu.Lock()
	defer s.attachmentMgr.mu.Unlock()

	seen := make(map[string]bool, len(ids))
	result := make([]domain.Attachment, 0, len(ids))
	payloads := make([]AIAttachmentPayload, 0, len(ids))
	var totalSize int64

	for _, id := range ids {
		if seen[id] {
			return nil, nil, fmt.Errorf("%w: 重複した添付ファイルIDが指定されています", ErrInvalid)
		}
		seen[id] = true

		staged, ok := s.attachmentMgr.staged[id]
		if !ok || staged.ChannelID != channelID || staged.UserID != userID {
			return nil, nil, fmt.Errorf("%w: 添付ファイルが見つからないか、有効期限が切れています", ErrNotFound)
		}

		totalSize += staged.Size
		if totalSize > MaxAttachmentTotalBytes {
			return nil, nil, fmt.Errorf("%w: 添付ファイルの合計サイズが上限（50 MiB）を超えています", ErrInvalid)
		}

		result = append(result, domain.Attachment{
			ID:       staged.ID,
			Filename: staged.Filename,
			Size:     staged.Size,
		})
		payloads = append(payloads, AIAttachmentPayload{
			Name:   staged.Filename,
			Base64: base64.StdEncoding.EncodeToString(staged.Data),
		})
	}

	// Remove consumed items from staging
	for _, id := range ids {
		delete(s.attachmentMgr.staged, id)
	}

	return result, payloads, nil
}
