package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/masapico/minihub/internal/service"
)

type incomingWebhookPayload struct {
	Text          string `json:"text"`
	ThreadRootSeq int64  `json:"threadRootSeq"`
	Attachments   []struct {
		Text     string `json:"text"`
		Fallback string `json:"fallback"`
	} `json:"attachments"`
}

func (a *API) handleIncomingWebhook(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	parts := strings.Split(strings.Trim(sub, "/"), "/")

	var webhookID, rawToken string
	if len(parts) == 2 {
		webhookID = parts[0]
		rawToken = parts[1]
	} else if len(parts) == 1 && parts[0] != "" {
		webhookID = parts[0]
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			rawToken = strings.TrimSpace(authHeader[7:])
		} else if custom := r.Header.Get("X-Webhook-Token"); custom != "" {
			rawToken = strings.TrimSpace(custom)
		}
	} else {
		a.writeJSON(w, http.StatusNotFound, map[string]string{"error": "Webhookのエンドポイントが見つかりません"})
		return
	}

	if rawToken == "" {
		a.writeError(w, service.ErrUnauthenticated)
		return
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	var text string
	var threadRootSeq int64

	limitedReader := io.LimitReader(r.Body, service.MaxMessageBytes+4096)
	if mediaType == "text/plain" {
		b, err := io.ReadAll(limitedReader)
		if err != nil {
			a.writeError(w, service.ErrInvalid)
			return
		}
		text = string(b)
	} else {
		var payload incomingWebhookPayload
		if err := json.NewDecoder(limitedReader).Decode(&payload); err != nil && err != io.EOF {
			a.writeError(w, service.ErrInvalid)
			return
		}
		text = payload.Text
		if text == "" && len(payload.Attachments) > 0 {
			var parts []string
			for _, att := range payload.Attachments {
				if att.Text != "" {
					parts = append(parts, att.Text)
				} else if att.Fallback != "" {
					parts = append(parts, att.Fallback)
				}
			}
			text = strings.Join(parts, "\n")
		}
		threadRootSeq = payload.ThreadRootSeq
	}

	msg, err := a.service.PostWebhookMessage(r.Context(), webhookID, rawToken, text, threadRootSeq)
	if err != nil {
		a.writeError(w, err)
		return
	}

	a.writeJSON(w, http.StatusCreated, map[string]any{
		"id":            msg.ID,
		"seq":           msg.Seq,
		"threadRootSeq": msg.ThreadRootSeq,
		"channelId":     msg.UserID,
	})
}

func (a *API) listAdminWebhooks(w http.ResponseWriter, r *http.Request, userID string) {
	list, err := a.service.ListAdminWebhooks(r.Context(), userID)
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.writeJSON(w, http.StatusOK, list)
}

func (a *API) getAdminWebhook(w http.ResponseWriter, r *http.Request, userID, id string) {
	item, err := a.service.GetAdminWebhook(r.Context(), userID, id)
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.writeJSON(w, http.StatusOK, item)
}

func (a *API) createAdminWebhook(w http.ResponseWriter, r *http.Request, userID string) {
	var in service.CreateWebhookInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		a.writeError(w, service.ErrInvalid)
		return
	}
	created, err := a.service.CreateWebhook(r.Context(), userID, in)
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.writeJSON(w, http.StatusCreated, created)
}

func (a *API) updateAdminWebhook(w http.ResponseWriter, r *http.Request, userID, id string) {
	var in service.UpdateWebhookInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		a.writeError(w, service.ErrInvalid)
		return
	}
	updated, err := a.service.UpdateWebhook(r.Context(), userID, id, in)
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.writeJSON(w, http.StatusOK, updated)
}

func (a *API) regenerateAdminWebhook(w http.ResponseWriter, r *http.Request, userID, id string) {
	result, err := a.service.RegenerateWebhookToken(r.Context(), userID, id)
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.writeJSON(w, http.StatusOK, result)
}

func (a *API) deleteAdminWebhook(w http.ResponseWriter, r *http.Request, userID, id string) {
	if err := a.service.DeleteWebhook(r.Context(), userID, id); err != nil {
		a.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

