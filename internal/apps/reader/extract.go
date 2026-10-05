package reader

import "github.com/iliafrenkel/on-suite/internal/platform/article"

// ExtractArticle is article.Extract bound to Reader's image proxy.
func ExtractArticle(body []byte, pageURL string) (article.Extracted, error) {
	return article.Extract(body, pageURL, readerImageSrc)
}
