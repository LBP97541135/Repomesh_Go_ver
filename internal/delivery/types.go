// Package delivery implements M6: after human release approval, the platform
// pushes the attempt workspace commits to the repository branch and opens the
// pull request using its own GitHub App installation credentials. Coding
// agents never hold SCM tokens (Py delivery 模块同哲学).
package delivery
