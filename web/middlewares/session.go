package middlewares

import (
	"github.com/cozy/cozy-stack/model/session"
	"github.com/labstack/echo/v4"
)

const sessionKey = "session"

// LoadSession is a middlewares that loads the session and stores it the
// request context.
func LoadSession(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		i, ok := GetInstanceSafe(c)
		if ok {
			sess, err := session.FromCookie(c, i)
			if err == nil {
				c.Set(sessionKey, sess)
			}
		}
		return next(c)
	}
}

// IsLoggedIn returns true if the context has a valid session cookie.
func IsLoggedIn(c echo.Context) bool {
	_, ok := GetSession(c)
	return ok
}

// GetSession returns the sessions associated with the given context.
func GetSession(c echo.Context) (sess *session.Session, ok bool) {
	v := c.Get(sessionKey)
	if v != nil {
		sess, ok = v.(*session.Session)
	}
	return sess, ok
}
