package errors

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cozy/cozy-stack/model/app"
	"github.com/cozy/cozy-stack/model/instance"
	build "github.com/cozy/cozy-stack/pkg/config"
	"github.com/cozy/cozy-stack/pkg/config/config"
	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/pkg/couchdb"
	"github.com/cozy/cozy-stack/pkg/jsonapi"
	"github.com/cozy/cozy-stack/pkg/logger"
	"github.com/cozy/cozy-stack/web/middlewares"

	"github.com/labstack/echo/v4"
)

// ErrorHandler is the default error handler of our APIs.
func ErrorHandler(err error, c echo.Context) {
	var je *jsonapi.Error
	var ce *couchdb.Error

	res := c.Response()
	req := c.Request()

	var ok bool
	if _, ok = err.(*echo.HTTPError); ok {
		// nothing to do
	} else if os.IsExist(err) {
		je = jsonapi.Conflict(err)
	} else if os.IsNotExist(err) {
		je = jsonapi.NotFound(err)
	} else if ce, ok = err.(*couchdb.Error); ok {
		je = &jsonapi.Error{
			Status: ce.StatusCode,
			Title:  ce.Name,
			Detail: ce.Reason,
		}
	} else if je, ok = err.(*jsonapi.Error); !ok {
		je = &jsonapi.Error{
			Status: http.StatusInternalServerError,
			Title:  "Unqualified error",
			Detail: err.Error(),
		}
	}

	if build.IsDevRelease() {
		var log *logger.Entry
		inst, ok := middlewares.GetInstanceSafe(c)
		if ok {
			log = inst.Logger().WithNamespace("http")
		} else {
			log = logger.WithNamespace("http")
		}
		log.Errorf("%s %s %s", req.Method, req.URL.Path, err)
	}

	if res.Committed {
		return
	}

	if je != nil {
		if req.Method == http.MethodHead {
			_ = c.NoContent(je.Status)
			return
		}
		_ = jsonapi.DataError(c, je)
		return
	}

	HTMLErrorHandler(err, c)
}

// HTMLErrorHandler is the default fallback error handler for error rendered in
// HTML pages, mainly for users, assets and routes that are not part of our API
// per-se.
func HTMLErrorHandler(err error, c echo.Context) {
	status := http.StatusInternalServerError
	req := c.Request()

	if build.IsDevRelease() {
		var log *logger.Entry
		inst, ok := middlewares.GetInstanceSafe(c)
		if ok {
			log = inst.Logger().WithNamespace("http")
		} else {
			log = logger.WithNamespace("http")
		}
		log.Errorf("%s %s %s", req.Method, req.URL.Path, err)
	}

	he, ok := err.(*echo.HTTPError)
	if ok {
		status = he.Code
		if he.Internal != nil {
			err = he.Internal
		}
	} else {
		he = echo.NewHTTPError(status, err)
		he.Internal = err
	}

	var title, value string
	switch err {
	case instance.ErrNotFound:
		status = http.StatusNotFound
		title = "Error Instance not found Title"
		value = "Error Instance not found Message"
	case app.ErrNotFound:
		status = http.StatusNotFound
		title = "Error Application not found Title"
		value = "Error Application not found Message"
	case app.ErrInvalidSlugName:
		status = http.StatusBadRequest
	}

	if title == "" {
		if status >= 500 {
			title = "Error Internal Server Error Title"
			value = "Error Internal Server Error Message"
		} else {
			title = "Error Title"
			value = fmt.Sprintf("%v", he.Message)
		}
	}

	accept := req.Header.Get("Accept")
	acceptHTML := strings.Contains(accept, echo.MIMETextHTML)
	acceptJSON := strings.Contains(accept, echo.MIMEApplicationJSON)
	if req.Method == http.MethodHead {
		err = c.NoContent(status)
	} else if acceptJSON {
		err = c.JSON(status, echo.Map{"error": he.Message})
	} else if acceptHTML {
		i, ok := middlewares.GetInstanceSafe(c)
		if !ok {
			i = &instance.Instance{
				Domain:      req.Host,
				ContextName: config.DefaultInstanceContext,
				Locale:      consts.DefaultLocale,
			}
		}

		inverted := false
		illustration := "/images/generic-error.svg"
		var link, linkURL, button, buttonURL string

		switch err {
		case instance.ErrNotFound:
			inverted = true
			illustration = "/images/desert.svg"
			link = "Error Address forgotten"
			linkURL = "https://manager.cozycloud.cc/v2/cozy/remind"
		case app.ErrNotFound:
			illustration = "/images/desert.svg"
			linkURL = i.DefaultRedirection().String()
			link = "Error Application not found Action"
			if ctxSettings, ok := i.SettingsContext(); ok {
				if hide, ok := ctxSettings["hide_button_on_app_not_found"].(bool); ok && hide {
					link = ""
				}
			}
		}

		err = c.Render(status, "error.html", echo.Map{
			"Domain":       i.ContextualDomain(),
			"ContextName":  i.ContextName,
			"Locale":       i.Locale,
			"Title":        i.TemplateTitle(),
			"Favicon":      middlewares.Favicon(i),
			"Inverted":     inverted,
			"Illustration": illustration,
			"ErrorTitle":   title,
			"Error":        value,
			"Link":         link,
			"LinkURL":      linkURL,
			"SupportEmail": i.SupportEmailAddress(),
			"Button":       button,
			"ButtonURL":    buttonURL,
		})
	} else {
		err = c.String(status, fmt.Sprintf("%v", he.Message))
	}

	if err != nil {
		logger.WithNamespace("http").Errorf("%s %s %s", req.Method, req.URL.Path, err)
	}
}
