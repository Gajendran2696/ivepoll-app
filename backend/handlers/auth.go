package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"livepoll/models"
	"livepoll/store"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// A real bcrypt hash of a random string. Compared against when the email is unknown so that
// "no such user" and "wrong password" take the same time (prevents user enumeration by timing).
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("livepoll-dummy-password"), bcryptCost)

const bcryptCost = 12

type signupReq struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *App) Signup(c *gin.Context) {
	var req signupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid request body")
		return
	}
	name := cleanText(req.Name)
	email := strings.ToLower(strings.TrimSpace(req.Email))

	switch {
	case runeLen(name) < 2 || runeLen(name) > 50:
		fail(c, http.StatusBadRequest, "name must be 2-50 characters")
		return
	case len(email) > 254 || !emailRe.MatchString(email):
		fail(c, http.StatusBadRequest, "enter a valid email address")
		return
	case len(req.Password) < 8:
		fail(c, http.StatusBadRequest, "password must be at least 8 characters")
		return
	case len(req.Password) > 72: // bcrypt silently ignores bytes beyond 72
		fail(c, http.StatusBadRequest, "password must be at most 72 bytes")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		abortServer(c, err)
		return
	}
	u := &models.User{Name: name, Email: email, PasswordHash: string(hash)}
	if err := a.Store.CreateUser(c.Request.Context(), u); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			fail(c, http.StatusConflict, "an account with this email already exists")
			return
		}
		abortServer(c, err)
		return
	}
	a.respondWithToken(c, http.StatusCreated, u)
}

func (a *App) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || len(req.Password) > 72 {
		fail(c, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	u, err := a.Store.UserByEmail(c.Request.Context(), email)
	hash := dummyHash
	if err == nil {
		hash = []byte(u.PasswordHash)
	} else if !errors.Is(err, store.ErrNotFound) {
		abortServer(c, err)
		return
	}
	pwErr := bcrypt.CompareHashAndPassword(hash, []byte(req.Password))
	if err != nil || pwErr != nil {
		fail(c, http.StatusUnauthorized, "incorrect email or password")
		return
	}
	a.respondWithToken(c, http.StatusOK, u)
}

func (a *App) Me(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "invalid session")
		return
	}
	u, err := a.Store.UserByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusUnauthorized, "account no longer exists")
			return
		}
		abortServer(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": userView(u)})
}

func (a *App) respondWithToken(c *gin.Context, status int, u *models.User) {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   u.ID.Hex(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(a.Cfg.TokenTTL)),
	})
	signed, err := tok.SignedString(a.Cfg.JWTSecret)
	if err != nil {
		abortServer(c, err)
		return
	}
	c.JSON(status, gin.H{"token": signed, "user": userView(u)})
}

func userView(u *models.User) models.UserView {
	return models.UserView{ID: u.ID.Hex(), Name: u.Name, Email: u.Email}
}
