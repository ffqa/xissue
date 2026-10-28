package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

const maxAttachmentBytes = 10 << 20

func attachmentURL(issueID, attachmentID int64) string {
	return fmt.Sprintf("/api/issues/%d/attachments/%d", issueID, attachmentID)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListAttachments(r.Context(), issueID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list {
		list[i].URL = attachmentURL(issueID, list[i].ID)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.st.GetIssue(r.Context(), issueID); err != nil {
		writeStoreErr(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes+1<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("empty file"))
		return
	}
	if len(data) > maxAttachmentBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("attachment exceeds %d MB", maxAttachmentBytes>>20))
		return
	}
	name := sanitizeAttachmentName(header.Filename)
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	dir := filepath.Join(s.cfg.AttachmentsDir(), strconv.FormatInt(issueID, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	stored := hex.EncodeToString(token[:]) + strings.ToLower(filepath.Ext(name))
	absPath := filepath.Join(dir, stored)
	if err := os.WriteFile(absPath, data, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	relPath, err := filepath.Rel(s.cfg.DataDir, absPath)
	if err != nil {
		_ = os.Remove(absPath)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	attachment, err := s.st.AddAttachment(r.Context(), domain.Attachment{
		IssueID: issueID,
		Name:    name,
		MIME:    mimeType,
		Size:    int64(len(data)),
	}, relPath)
	if err != nil {
		_ = os.Remove(absPath)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	attachment.URL = attachmentURL(issueID, attachment.ID)
	_ = s.st.AddIssueEvent(r.Context(), issueID, "attachment", "me", "上传附件 "+name)
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	attachmentID, err := strconv.ParseInt(r.PathValue("attachmentID"), 10, 64)
	if err != nil || attachmentID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("invalid attachment id"))
		return
	}
	attachment, relPath, err := s.st.GetAttachment(r.Context(), issueID, attachmentID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	absPath := filepath.Join(s.cfg.DataDir, relPath)
	base, _ := filepath.Abs(s.cfg.DataDir)
	target, _ := filepath.Abs(absPath)
	if target != base && !strings.HasPrefix(target, base+string(filepath.Separator)) {
		writeErr(w, http.StatusNotFound, errors.New("attachment not found"))
		return
	}
	f, err := os.Open(target)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("attachment not found"))
		return
	}
	defer f.Close()
	inline := isInlineImage(attachment.MIME)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", attachment.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	dispositionValue := mime.FormatMediaType(disposition, map[string]string{"filename": strings.ReplaceAll(attachment.Name, "\"", "")})
	if dispositionValue == "" {
		dispositionValue = disposition
	}
	w.Header().Set("Content-Disposition", dispositionValue)
	http.ServeContent(w, r, attachment.Name, attachment.CreatedAt, f)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	attachmentID, err := strconv.ParseInt(r.PathValue("attachmentID"), 10, 64)
	if err != nil || attachmentID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("invalid attachment id"))
		return
	}
	relPath, err := s.st.DeleteAttachment(r.Context(), issueID, attachmentID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	_ = os.Remove(filepath.Join(s.cfg.DataDir, relPath))
	_ = s.st.AddIssueEvent(r.Context(), issueID, "attachment", "me", "删除附件")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func sanitizeAttachmentName(name string) string {
	name = filepath.Base(strings.TrimSpace(strings.ReplaceAll(name, "\\", "/")))
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		name = name[:180-len(ext)] + ext
	}
	return name
}

func isInlineImage(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml"
}
