package handlers

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var uploadTmpl = template.Must(template.New("upload").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Upload avatar</title></head><body>
<h1>Upload avatar</h1>
<form action="/web/upload" method="post" enctype="multipart/form-data">
  <label>User ID: <input type="text" name="user_id" required></label><br>
  <label>File: <input type="file" name="file" required></label><br>
  <button type="submit">Upload</button>
</form>
</body></html>`))

var galleryTmpl = template.Must(template.New("gallery").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Gallery</title></head><body>
<h1>Gallery — {{.UserID}}</h1>
<ul>
{{range .Avatars}}
  <li>
    <img src="/api/v1/avatars/{{.ID}}" width="100" alt="{{.FileName}}">
    <code>{{.ID}}</code>
    <form action="/web/avatars/{{.ID}}/delete" method="post" style="display:inline"
          onsubmit="return confirm('Delete this avatar?')">
      <input type="hidden" name="user_id" value="{{$.UserID}}">
      <button type="submit">Delete</button>
    </form>
  </li>
{{end}}
</ul>
<p><a href="/web/upload">Upload another</a></p>
</body></html>`))

// WebUploadForm handles GET /web/upload.
func (h *Handler) WebUploadForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uploadTmpl.Execute(w, nil); err != nil {
		h.logger.Error("handler: render upload form failed", "error", err)
	}
}

// WebUpload handles POST /web/upload.
func (h *Handler) WebUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize)
	if err := r.ParseMultipartForm(h.maxUploadSize); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	userID := r.FormValue("user_id")
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	av, err := h.service.UploadAvatar(r.Context(), userID, file, header.Filename, ct, header.Size)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	http.Redirect(w, r, "/web/gallery/"+av.UserID, http.StatusSeeOther)
}

// WebGallery handles GET /web/gallery/{user_id}.
func (h *Handler) WebGallery(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	avatars, err := h.service.ListUserAvatars(r.Context(), userID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := galleryTmpl.Execute(w, map[string]any{
		"UserID":  userID,
		"Avatars": avatars,
	}); err != nil {
		h.logger.Error("handler: render gallery failed", "error", err)
	}
}

// WebDeleteAvatar handles POST /web/avatars/{avatar_id}/delete.
// Browsers cannot send DELETE from a plain form, so this is a POST
// that performs the deletion and redirects back to the gallery.
func (h *Handler) WebDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatar_id"))
	if err != nil {
		http.Error(w, "invalid avatar id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	userID := r.FormValue("user_id")
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.DeleteAvatar(r.Context(), id, userID); err != nil {
		h.handleServiceError(w, err)
		return
	}
	http.Redirect(w, r, "/web/gallery/"+userID, http.StatusSeeOther)
}
