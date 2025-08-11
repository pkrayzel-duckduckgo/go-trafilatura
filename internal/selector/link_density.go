package selector

import (
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
)

func IsWhitelistedCategoryList(n *html.Node) bool {
	class := dom.ClassName(n)
	id := dom.ID(n)
	return strings.Contains(class, "mw-category") ||
		strings.Contains(id, "mw-pages") ||
		strings.Contains(class, "mw-category-group") ||
		strings.Contains(id, "mw-subcategories")
}
