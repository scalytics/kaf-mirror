package main

import (
	"bufio"
	"fmt"
	"html"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func main() {
	fmt.Println("Generating CLI documentation...")

	// Generate Markdown
	markdownFile := "./docs/cli-commands.md"
	cmd := exec.Command("go", "run", "./cmd/mirror-cli", "docs", "generate", "--file", markdownFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error generating markdown docs: %v\n", err)
		os.Exit(1)
	}

	// Convert to HTML
	htmlFile := "./web/docu/cli-commands.html"
	if err := convertMarkdownToHTML(markdownFile, htmlFile); err != nil {
		fmt.Printf("Error converting markdown to HTML: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("CLI documentation generated successfully!")
}

// convertMarkdownToHTML converts the comprehensive Markdown file to HTML
func convertMarkdownToHTML(markdownFile, htmlFile string) error {
	file, err := os.Open(markdownFile)
	if err != nil {
		return fmt.Errorf("failed to open markdown file: %v", err)
	}
	defer file.Close()

	htmlOutput, err := os.Create(htmlFile)
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %v", err)
	}
	defer htmlOutput.Close()

	htmlOutput.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>CLI Command Reference - kaf-mirror</title>
    <link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css">
    <style>
        :root {
            --primary-color: #667eea;
            --secondary-color: #764ba2;
            --background-color: #f8f9fa;
            --card-background: #ffffff;
            --text-color: #34495e;
            --header-text-color: #ffffff;
            --nav-link-color: #566573;
            --nav-link-hover-bg: #eef2f7;
            --nav-link-active-bg: #e2e8f0;
            --nav-link-active-color: #2c3e50;
            --code-bg: #2d3748;
            --code-color: #e2e8f0;
            --border-color: #e2e8f0;
        }
        * {
            box-sizing: border-box;
        }
        html {
            scroll-behavior: smooth;
        }
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif; 
            line-height: 1.6;
            margin: 0;
            padding-top: 5rem;
            color: var(--text-color);
            background-color: var(--background-color);
        }
        .container-fluid {
            display: flex;
        }
        .sidebar {
            width: 300px;
            background: var(--card-background);
            border-right: 1px solid var(--border-color);
            padding: 25px;
            position: fixed;
            height: calc(100vh - 5rem);
            overflow-y: auto;
        }
        .sidebar .logo {
            font-size: 1.5rem;
            font-weight: 600;
            color: var(--primary-color);
            margin: 0 0 25px 0;
            padding-bottom: 15px;
            border-bottom: 2px solid var(--primary-color);
        }
        .sidebar ul {
            list-style: none;
            padding: 0;
            margin: 0;
        }
        .sidebar a {
            display: block;
            padding: 8px 12px;
            color: var(--nav-link-color);
            text-decoration: none;
            border-radius: 6px;
            font-size: 0.9rem;
            transition: all 0.2s ease;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .sidebar a:hover {
            background-color: var(--nav-link-hover-bg);
        }
        .sidebar a.active {
            background-color: var(--nav-link-active-bg);
            color: var(--nav-link-active-color);
            font-weight: 500;
        }
        .sidebar .level-1 { font-weight: 500; }
        .sidebar .level-2 { padding-left: 1.5rem; }
        .sidebar .level-3 { padding-left: 3rem; }
        .sidebar .level-4 { padding-left: 4.5rem; }
        .sidebar .level-5 { padding-left: 6rem; }

        .content {
            flex: 1;
            margin-left: 300px;
            padding: 40px;
            max-width: calc(100% - 300px);
        }
        .command-card {
            background: var(--card-background);
            border-radius: 8px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.08);
            margin-bottom: 40px;
            padding: 30px;
            border-top: 4px solid var(--primary-color);
            scroll-margin-top: 20px;
        }
        .command-card h1, .command-card h2, .command-card h3, .command-card h4, .command-card h5, .command-card h6 {
            font-weight: 600;
            color: var(--text-color);
            margin: 0 0 10px 0;
        }
        .command-card h1 { font-size: 2rem; }
        .command-card h2 { font-size: 1.8rem; }
        .command-card h3 { font-size: 1.5rem; color: var(--secondary-color); margin-top: 30px; }
        .command-card h4 { font-size: 1.2rem; }
        .command-card h5 { font-size: 1.1rem; }
        .command-card h6 { font-size: 1rem; }
        .command-card p {
            margin: 10px 0;
            color: var(--text-color);
        }
        .command-card pre {
            background-color: var(--code-bg);
            color: var(--code-color);
            padding: 15px;
            border-radius: 6px;
            font-family: 'Courier New', Courier, monospace;
            font-size: 1em;
            margin: 15px 0;
            white-space: pre-wrap;
            word-break: break-all;
            overflow-x: auto;
        }
        .command-card code {
            background-color: var(--code-bg);
            color: var(--code-color);
            padding: 2px 4px;
            border-radius: 3px;
            font-family: 'Courier New', Courier, monospace;
            font-size: 0.9em;
        }
        .command-card .usage {
            background-color: var(--code-bg);
            color: var(--code-color);
            padding: 15px;
            border-radius: 6px;
            font-family: 'Courier New', Courier, monospace;
            font-size: 1em;
            margin: 15px 0;
            white-space: pre-wrap;
            word-break: break-all;
        }
        .command-card .flags-list {
            list-style: none;
            padding: 0;
            margin: 10px 0;
        }
        .command-card .flags-list li {
            background-color: var(--background-color);
            padding: 10px;
            border-radius: 4px;
            font-family: 'Courier New', Courier, monospace;
            font-size: 1em;
            margin-bottom: 8px;
            border-left: 3px solid var(--secondary-color);
        }
        .command-card .description {
            margin: 15px 0;
            padding: 15px;
            background: #fdfdff;
            border-left: 3px solid var(--primary-color);
            border-radius: 4px;
        }
        .navbar { border-bottom: 1px solid #ddd; }
        @media (max-width: 900px) {
            .sidebar {
                display: none;
            }
            .content {
                margin-left: 0;
                max-width: 100%;
                padding: 20px;
            }
        }
    </style>
</head>
<body>
    <nav class="navbar navbar-expand-md navbar-light bg-light fixed-top">
        <div class="container">
            <a class="navbar-brand" href="/docu/index.html">kaf-mirror Documentation</a>
            <button class="navbar-toggler" type="button" data-toggle="collapse" data-target="#navbarsExampleDefault" aria-controls="navbarsExampleDefault" aria-expanded="false" aria-label="Toggle navigation">
                <span class="navbar-toggler-icon"></span>
            </button>
            <div class="collapse navbar-collapse" id="navbarsExampleDefault">
                <ul class="navbar-nav ml-auto">
                    <li class="nav-item">
                        <a class="nav-link" href="/docu/index.html">Home</a>
                    </li>
                    <li class="nav-item active">
                        <a class="nav-link" href="/docu/cli-commands.html">CLI Commands<span class="sr-only">(current)</span></a>
                    </li>
                    <li class="nav-item">
                        <a class="nav-link" href="/docu/rbac.html">RBAC</a>
                    </li>
                    <li class="nav-item">
                        <a class="nav-link" href="/docu/swagger/">API (Swagger)</a>
                    </li>
                </ul>
            </div>
        </div>
    </nav>
    <div class="container-fluid">
        <nav class="sidebar">
            <div class="logo">CLI Reference</div>
            <ul id="nav-list"></ul>
        </nav>
        <main class="content">
`)

	// First pass: collect headers for navigation
	var navItems []NavItem
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if level := getHeaderLevel(line); level > 0 {
			title := strings.TrimSpace(line[level:])
			// Only include command definitions in the navigation
			if strings.HasPrefix(title, "mirror-cli") {
				anchor := generateAnchor(title)
				navItems = append(navItems, NavItem{
					Title:  title,
					Anchor: anchor,
					Level:  level,
				})
			}
		}
	}
	file.Close()

	// Generate navigation and write to a temporary buffer
	navHTML := generateNavigation(navItems)

	// Re-open file for second pass
	file, err = os.Open(markdownFile)
	if err != nil {
		return fmt.Errorf("failed to reopen markdown file: %v", err)
	}
	defer file.Close()

	// Write the main content
	scanner = bufio.NewScanner(file)
	inCodeBlock := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				htmlOutput.WriteString("</code></pre>\n")
				inCodeBlock = false
			} else {
				htmlOutput.WriteString("<pre><code>")
				inCodeBlock = true
			}
			continue
		}
		if inCodeBlock {
			htmlOutput.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		htmlLine := convertMarkdownLine(line)
		htmlOutput.WriteString(htmlLine + "\n")
	}
	if inCodeBlock {
		htmlOutput.WriteString("</code></pre>\n")
	}

	// Close the last command card
	htmlOutput.WriteString("</div>\n")

	htmlOutput.WriteString(`
        </main>
    </div>
    <script src="https://code.jquery.com/jquery-3.5.1.slim.min.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/@popperjs/core@2.5.4/dist/umd/popper.min.js"></script>
    <script src="https://stackpath.bootstrapcdn.com/bootstrap/4.5.2/js/bootstrap.min.js"></script>
    <script>
		// Populate navigation
		document.getElementById('nav-list').innerHTML = '` + navHTML + `';

        // Smooth scrolling for anchor links
        document.querySelectorAll('a[href^="#"]').forEach(anchor => {
            anchor.addEventListener('click', function (e) {
                e.preventDefault();
                const target = document.querySelector(this.getAttribute('href'));
                if (target) {
                    target.scrollIntoView({
                        behavior: 'smooth',
                        block: 'start'
                    });
                }
            });
        });
        
        // Highlight current section in navigation
        const observer = new IntersectionObserver((entries) => {
            entries.forEach((entry) => {
                const id = entry.target.getAttribute('id');
                const navLink = document.querySelector('.sidebar a[href="#' + id + '"]');
                if (entry.isIntersecting) {
                    document.querySelectorAll('.sidebar a').forEach(link => link.classList.remove('active'));
                    if (navLink) {
                        navLink.classList.add('active');
                    }
                }
            });
        }, { rootMargin: '-30% 0px -70% 0px' });
        
        document.querySelectorAll('.command-card').forEach(card => {
            observer.observe(card);
        });
    </script>
</body>
</html>
`)

	return scanner.Err()
}

// NavItem represents a navigation item
type NavItem struct {
	Title  string
	Anchor string
	Level  int
}

// convertMarkdownLine converts a single line of markdown to HTML
func convertMarkdownLine(line string) string {
	if line == "" {
		return "<br>"
	}

	// Headers with anchors
	if level := getHeaderLevel(line); level > 0 {
		title := strings.TrimSpace(line[level:])
		anchor := generateAnchor(title)

		// Check if this is a main command (starts with "mirror-cli" and is h1 or h3)
		isMainCommand := (level == 1 || level == 3) && strings.HasPrefix(title, "mirror-cli")

		if isMainCommand {
			if level == 1 {
				return fmt.Sprintf(`<div class="command-card" id="%s"><h1 id="%s">%s</h1>`, anchor, anchor, html.EscapeString(title))
			} else {
				return fmt.Sprintf(`</div><div class="command-card" id="%s"><h3 id="%s">%s</h3>`, anchor, anchor, html.EscapeString(title))
			}
		}
		return fmt.Sprintf(`<h%d id="%s">%s</h%d>`, level, anchor, html.EscapeString(title), level)
	}

	// Horizontal rule
	if strings.TrimSpace(line) == "---" {
		return "<hr>"
	}

	// Lists
	if strings.HasPrefix(line, "- ") {
		return "<li>" + processInlineMarkdown(html.EscapeString(strings.TrimSpace(line[2:]))) + "</li>"
	}

	// Regular paragraph
	return "<p>" + processInlineMarkdown(html.EscapeString(line)) + "</p>"
}

// processInlineMarkdown processes inline markdown like bold, code, etc.
func processInlineMarkdown(text string) string {
	boldRe := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRe.ReplaceAllString(text, "<strong>$1</strong>")

	codeRe := regexp.MustCompile("`([^`]+)`")
	text = codeRe.ReplaceAllString(text, "<code>$1</code>")

	return text
}

// getHeaderLevel returns the level of a markdown header (1-6), or 0 if not a header
func getHeaderLevel(line string) int {
	if strings.HasPrefix(line, "######") {
		return 6
	} else if strings.HasPrefix(line, "#####") {
		return 5
	} else if strings.HasPrefix(line, "####") {
		return 4
	} else if strings.HasPrefix(line, "###") {
		return 3
	} else if strings.HasPrefix(line, "##") {
		return 2
	} else if strings.HasPrefix(line, "#") {
		return 1
	}
	return 0
}

// generateAnchor generates a URL-safe anchor from a title
func generateAnchor(title string) string {
	anchor := strings.ToLower(title)
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	anchor = reg.ReplaceAllString(anchor, "-")
	return strings.Trim(anchor, "-")
}

// generateNavigation generates the sidebar navigation HTML
func generateNavigation(navItems []NavItem) string {
	var nav strings.Builder
	for _, item := range navItems {
		levelClass := fmt.Sprintf("level-%d", item.Level)
		nav.WriteString(fmt.Sprintf(`<li><a href="#%s" class="%s">`, item.Anchor, levelClass))
		nav.WriteString(html.EscapeString(item.Title))
		nav.WriteString(`</a></li>`)
	}
	return nav.String()
}
