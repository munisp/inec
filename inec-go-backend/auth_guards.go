package main

import (
	"net/http"
)

// authRequired wraps a handler to require any authenticated user.
func authRequired(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := getUserFromContext(r); !ok {
			writeError(w, 401, "authentication required")
			return
		}
		handler(w, r)
	}
}

// roleRequired wraps a handler to require specific roles.
func roleRequired(handler http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := guardRole(w, r, roles...); !ok {
			return
		}
		handler(w, r)
	}
}

// adminOnly is a shorthand for requiring admin role.
func adminOnly(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin")
}

// staffOnly requires admin, presiding_officer, or collation_officer roles.
func staffOnly(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer")
}

// readAuth requires any authenticated user for read operations on sensitive data.
func readAuth(handler http.HandlerFunc) http.HandlerFunc {
	return authRequired(handler)
}

// writeAuth requires admin or specific officer roles for write operations.
func writeAuth(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer")
}

// adminOrOfficer requires admin or any officer role for management operations.
func adminOrOfficer(handler http.HandlerFunc) http.HandlerFunc {
	return roleRequired(handler, "admin", "presiding_officer", "collation_officer", "observer")
}

// handlePromoteUser allows admins to assign elevated roles to existing users.
func handlePromoteUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := guardRole(w, r, "admin"); !ok {
		return
	}
	var req struct {
		UserID int    `json:"user_id" validate:"required,gt=0"`
		Role   string `json:"role" validate:"required,oneof=admin presiding_officer collation_officer observer public"`
	}
	if !decodeAndValidateBody(w, r, &req) {
		return
	}
	dbExecCtx(r.Context(), "UPDATE users SET role=? WHERE id=?", req.Role, req.UserID)
	auditWrite("USER_PROMOTED", "user", "", r, map[string]interface{}{"user_id": req.UserID, "new_role": req.Role})
	writeJSON(w, 200, M{"message": "User role updated", "role": req.Role})
}
