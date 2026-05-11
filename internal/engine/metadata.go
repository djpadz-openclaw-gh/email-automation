package engine

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	lua "github.com/yuin/gopher-lua"

	"github.com/djpadz/email-automation/internal/db"
	"github.com/djpadz/email-automation/internal/models"
)

// MetadataContext holds the dependencies needed for metadata Lua functions.
type MetadataContext struct {
	DB       *db.DB
	TenantID int64
}

// registerMetadataFunctions adds metadata helper functions to the Lua environment.
// These allow rules to attach, query, and remove metadata tags on emails.
func registerMetadataFunctions(L *lua.LState, metaCtx *MetadataContext, email *models.EmailContext) {
	if metaCtx == nil {
		// Register no-op functions if metadata context is not available
		registerMetadataNoOps(L)
		return
	}

	// attach_metadata(email_id, key, value) — attach metadata to an email
	L.SetGlobal("attach_metadata", L.NewFunction(func(L *lua.LState) int {
		emailID := L.CheckString(1)
		key := L.CheckString(2)
		value := L.OptString(3, "")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := metaCtx.DB.AttachMetadata(ctx, metaCtx.TenantID, emailID, key, value)
		if err != nil {
			log.Error().Err(err).
				Str("email_id", emailID).
				Str("key", key).
				Msg("attach_metadata failed")
			L.Push(lua.LBool(false))
			return 1
		}

		L.Push(lua.LBool(true))
		return 1
	}))

	// query_by_metadata(key, value, options?) — query emails by metadata
	// options is an optional table with: exclude_id (string)
	// Returns a Lua table of message IDs
	L.SetGlobal("query_by_metadata", L.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(1)
		value := L.OptString(2, "")

		var excludeID string

		// Parse optional options table
		if L.GetTop() >= 3 {
			optsTbl := L.OptTable(3, nil)
			if optsTbl != nil {
				if exc := optsTbl.RawGetString("exclude_id"); exc != lua.LNil {
					excludeID = exc.String()
				}
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		records, err := metaCtx.DB.QueryByMetadata(ctx, metaCtx.TenantID, key, value, excludeID)
		if err != nil {
			log.Error().Err(err).
				Str("key", key).
				Str("value", value).
				Msg("query_by_metadata failed")
			// Return empty table on error
			L.Push(L.NewTable())
			return 1
		}

		// Return a table of message IDs
		resultTbl := L.NewTable()
		for i, r := range records {
			resultTbl.RawSetInt(i+1, lua.LString(r.MessageID))
		}
		L.Push(resultTbl)
		return 1
	}))

	// remove_metadata(email_id, key) — remove a metadata tag from an email
	L.SetGlobal("remove_metadata", L.NewFunction(func(L *lua.LState) int {
		emailID := L.CheckString(1)
		key := L.CheckString(2)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := metaCtx.DB.RemoveMetadata(ctx, metaCtx.TenantID, emailID, key)
		if err != nil {
			log.Error().Err(err).
				Str("email_id", emailID).
				Str("key", key).
				Msg("remove_metadata failed")
			L.Push(lua.LBool(false))
			return 1
		}

		L.Push(lua.LBool(true))
		return 1
	}))
}

// registerMetadataNoOps registers metadata functions that log warnings and return false/empty.
// Used when no database context is available (e.g., during dry-run or testing).
func registerMetadataNoOps(L *lua.LState) {
	L.SetGlobal("attach_metadata", L.NewFunction(func(L *lua.LState) int {
		log.Warn().Msg("attach_metadata called but metadata context is not available")
		L.Push(lua.LBool(false))
		return 1
	}))

	L.SetGlobal("query_by_metadata", L.NewFunction(func(L *lua.LState) int {
		log.Warn().Msg("query_by_metadata called but metadata context is not available")
		L.Push(L.NewTable())
		return 1
	}))

	L.SetGlobal("remove_metadata", L.NewFunction(func(L *lua.LState) int {
		log.Warn().Msg("remove_metadata called but metadata context is not available")
		L.Push(lua.LBool(false))
		return 1
	}))
}
