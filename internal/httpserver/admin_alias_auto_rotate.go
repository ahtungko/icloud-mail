package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"icloud-api/internal/domain"
	"icloud-api/internal/store"
)

type adminAPISetAliasAutoRotateRequest struct {
	DurationMinutes int `json:"duration_minutes"`
}

func (s *Server) adminAPISetAliasAutoRotate(c *gin.Context) {
	id, ok := adminAPIParseID(c)
	if !ok {
		return
	}
	var input adminAPISetAliasAutoRotateRequest
	if !decodeAdminAPIJSON(c, &input) {
		return
	}
	if input.DurationMinutes < 0 || input.DurationMinutes > 10080 {
		writeAdminAPIError(c, http.StatusBadRequest, "VALIDATION_FAILED", "轮换时间必须在 0 到 10080 分钟之间")
		return
	}
	alias, err := s.store.GetAlias(c.Request.Context(), id)
	if err != nil {
		s.writeAdminAPIStoreReadError(c, err)
		return
	}
	if input.DurationMinutes > 0 && alias.CredentialMode != domain.AliasCredentialModeV2 {
		writeAdminAPIError(c, http.StatusConflict, "ALIAS_CREDENTIAL_MODE_UNSUPPORTED", "旧版隐私邮箱只支持升级为整套凭据后设置自动轮换")
		return
	}

	var updated domain.Alias
	err = s.withAccountLock(c.Request.Context(), alias.AccountID, func() error {
		var setErr error
		updated, setErr = s.store.SetAliasAutoRotate(c.Request.Context(), id, int64(input.DurationMinutes)*60)
		return setErr
	})
	if err != nil {
		s.writeAdminAPIStoreReadError(c, err)
		return
	}

	session := mustSession(c)
	actionDetail := "关闭整套凭证自动轮换"
	if input.DurationMinutes > 0 {
		actionDetail = fmt.Sprintf("设置接码后 %d 分钟自动轮换整套凭证", input.DurationMinutes)
	}
	s.audit(c, &session.AdminID, session.Username, "set_alias_auto_rotate", "alias", strconv.FormatInt(id, 10), "success", actionDetail)

	aliasDTO, err := s.adminAPIAliasFromDomain(updated)
	if err != nil {
		s.writeAdminAPIInternalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	writeAdminAPIData(c, http.StatusOK, aliasDTO)
}

func (s *Server) RunAliasAutoRotation(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.processDueAliasAutoRotations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) processDueAliasAutoRotations(ctx context.Context) {
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	due, err := s.store.ListDueAutoRotateAliases(ctx, now, 50)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("查询待自动轮换隐私邮箱失败", "error", err)
		}
		return
	}
	for _, alias := range due {
		if ctx.Err() != nil {
			return
		}
		s.rotateDueAlias(ctx, alias)
	}
}

func (s *Server) rotateDueAlias(ctx context.Context, target domain.Alias) {
	accountID := target.AccountID
	aliasID := target.ID
	err := s.withAccountLock(ctx, accountID, func() error {
		current, err := s.store.GetAlias(ctx, aliasID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		if s.now != nil {
			now = s.now().UTC()
		}
		if current.AutoRotateDeadline == nil || current.AutoRotateDeadline.After(now) {
			return nil
		}
		if current.CredentialMode != domain.AliasCredentialModeV2 {
			_ = s.store.ClearAliasAutoRotate(ctx, aliasID)
			return errors.New("alias is not v2 credential mode, auto rotate skipped")
		}
		rotated, err := s.store.RotateAliasCredentials(ctx, aliasID)
		if err != nil {
			return err
		}
		s.logger.Info("隐私邮箱整套凭据已自动轮换", "alias_id", aliasID, "address", rotated.Address, "version", rotated.CredentialVersion)
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.store.CreateAuditLog(auditCtx, domain.AuditLog{
			Username:     "system",
			Action:       "auto_rotate_credentials",
			ResourceType: "alias",
			ResourceID:   strconv.FormatInt(aliasID, 10),
			Result:       "success",
			Detail:       "到达设定时间自动轮换整套凭据",
			CreatedAt:    time.Now().UTC(),
		})
		return nil
	})
	if err != nil && ctx.Err() == nil {
		s.logger.Error("执行隐私邮箱自动凭据轮换失败", "alias_id", aliasID, "error", err)
	}
}
