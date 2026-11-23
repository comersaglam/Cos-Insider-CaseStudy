package notification

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// templateRequest is the body of POST /v1/templates and PUT /v1/templates/{id}: the whole template.
type templateRequest struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
	Body    string `json:"body"`
}

func (req templateRequest) draft() TemplateDraft {
	return TemplateDraft{Name: req.Name, Channel: Channel(req.Channel), Body: req.Body}
}

// templateResponse is a template as the API shows it, with the variables its body needs.
type templateResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Channel   Channel   `json:"channel"`
	Body      string    `json:"body"`
	Variables []string  `json:"variables"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toTemplateResponse(t Template) templateResponse {
	variables := t.Variables()
	if variables == nil {
		variables = []string{} // a body without placeholders has none: [], not null
	}
	return templateResponse{
		ID: t.ID, Name: t.Name, Channel: t.Channel, Body: t.Body, Variables: variables,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// templateListResponse is a page of templates; nextCursor, absent on the last page, asks for the next one.
type templateListResponse struct {
	Items      []templateResponse `json:"items"`
	NextCursor string             `json:"nextCursor,omitzero"`
}

// createTemplate answers POST /v1/templates: 201 with the template, or 409 when its name is taken.
func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) error {
	var req templateRequest
	if err := httpx.DecodeJSON(w, r, maxCreateBody, &req); err != nil {
		return err
	}
	t, err := h.svc.CreateTemplate(r.Context(), req.draft())
	if err != nil {
		return toProblem(err)
	}
	w.Header().Set("Location", "/v1/templates/"+t.ID.String())
	return httpx.WriteJSON(w, http.StatusCreated, toTemplateResponse(t))
}

// listTemplates answers a page of templates, newest first.
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) error {
	after, limit, err := parsePageQuery(r.URL.Query())
	if err != nil {
		return err
	}
	page, err := h.svc.ListTemplates(r.Context(), after, limit)
	if err != nil {
		return toProblem(err)
	}
	resp := templateListResponse{Items: make([]templateResponse, 0, len(page.Items))}
	for _, t := range page.Items {
		resp.Items = append(resp.Items, toTemplateResponse(t))
	}
	if page.Next != nil {
		resp.NextCursor = encodeCursor(*page.Next)
	}
	return httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	t, err := h.svc.Template(r.Context(), id)
	if err != nil {
		return toProblem(err)
	}
	return httpx.WriteJSON(w, http.StatusOK, toTemplateResponse(t))
}

// updateTemplate answers PUT /v1/templates/{id}, which replaces the whole template.
func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	var req templateRequest
	if err := httpx.DecodeJSON(w, r, maxCreateBody, &req); err != nil {
		return err
	}
	t, err := h.svc.UpdateTemplate(r.Context(), id, req.draft())
	if err != nil {
		return toProblem(err)
	}
	return httpx.WriteJSON(w, http.StatusOK, toTemplateResponse(t))
}

// deleteTemplate answers DELETE /v1/templates/{id} with 204 and no body.
func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	if err := h.svc.DeleteTemplate(r.Context(), id); err != nil {
		return toProblem(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// parsePageQuery reads a list's cursor and page size, the only parameters a list of templates takes, reporting
// every bad parameter at once.
func parsePageQuery(q url.Values) (after *Cursor, limit int, err error) {
	limit = defaultPageSize
	var problems []httpx.FieldProblem
	bad := func(parameter, detail string) {
		problems = append(problems, httpx.FieldProblem{Parameter: parameter, Code: string(CodeInvalid), Detail: detail})
	}
	for _, name := range slices.Sorted(maps.Keys(q)) {
		switch v := q[name][0]; {
		case name != "limit" && name != "cursor":
			bad(name, "is not a parameter of this list")
		case len(q[name]) > 1:
			bad(name, "must be given once")
		case name == "limit":
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxPageSize {
				limit = n
			} else {
				bad(name, "must be a whole number from 1 to "+strconv.Itoa(maxPageSize))
			}
		default:
			if c, ok := decodeCursor(v); ok {
				after = &c
			} else {
				bad(name, "is not a cursor this API gave out")
			}
		}
	}
	if len(problems) > 0 {
		p := httpx.NewProblem(http.StatusBadRequest, "invalid_query", "the query is invalid")
		p.Errors = problems
		return nil, 0, p
	}
	return after, limit, nil
}
