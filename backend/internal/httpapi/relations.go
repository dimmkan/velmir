package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"velmir/backend/internal/authorization"
	"velmir/backend/internal/records"
)

const maxRelationResolveBodySize = 256 * 1024

type relationResolver interface {
	ResolveRelations(context.Context, []records.RelationResolvePlan) (records.RelationResolveResult, error)
}

func resolveRelations(inspector schemaInspector, lister recordLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resolver, ok := lister.(relationResolver)
		if inspector == nil || !ok {
			writeTargetNotConfigured(w)
			return
		}
		request, ok := decodeRelationResolveRequest(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), recordsTimeout)
		defer cancel()
		catalog, err := inspector.Inspect(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{
				Code:    "SCHEMA_INTROSPECTION_FAILED",
				Message: "Could not read target database schema",
			}})
			return
		}
		current, found := catalog.FindResource(chi.URLParam(r, "schema"), chi.URLParam(r, "resource"))
		if !found {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: apiError{
				Code:    "RESOURCE_NOT_FOUND",
				Message: "Resource is not present in the target database schema",
			}})
			return
		}
		permissions := currentPermissions(r)
		plans, err := records.PrepareRelationResolve(request, current, chi.URLParam(r, "schema"), catalog, func(schema, resource string) bool {
			return permissions.Allows(schema, resource, authorization.Read)
		}, func(schema, resource, column string) bool {
			metadata, ok := catalog.FindResource(schema, resource)
			return ok && readableColumn(permissions, schema, metadata, column)
		})
		if errors.Is(err, records.ErrRelationResolveForbidden) {
			writeForbidden(w)
			return
		}
		if err != nil {
			var validationErr *records.ValidationError
			if errors.As(err, &validationErr) {
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{
					Code:    validationErr.Code,
					Message: validationErr.Message,
				}})
				return
			}
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{
				Code:    "INVALID_RELATION_RESOLVE",
				Message: "Invalid relation resolve request",
			}})
			return
		}
		result, err := resolver.ResolveRelations(ctx, plans)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: apiError{
				Code:    "RELATION_RESOLVE_FAILED",
				Message: "Could not resolve related records",
			}})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func decodeRelationResolveRequest(w http.ResponseWriter, r *http.Request) (records.RelationResolveRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRelationResolveBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request records.RelationResolveRequest
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRelationResolveRequest(w)
		return records.RelationResolveRequest{}, false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeInvalidRelationResolveRequest(w)
		return records.RelationResolveRequest{}, false
	}
	if request.Relations == nil {
		request.Relations = make([]records.RelationResolveRelation, 0)
	}
	return request, true
}

func writeInvalidRelationResolveRequest(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: apiError{
		Code:    "INVALID_RELATION_RESOLVE",
		Message: "Request body must contain one relations object",
	}})
}
