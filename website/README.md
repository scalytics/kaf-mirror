# kaf-mirror website

This directory is the source of the project site served at
<https://kafmirror.scalytics.io/>.

The pages are plain HTML and CSS, assembled by a short Python script
with no extra packages. `build.sh` writes the site to `_site/` and wraps
the generated CLI reference from `web/docu/cli-commands.html` with the same
header and footer.

```bash
./website/build.sh
python3 -m http.server --directory _site 4173
```

Then open `http://localhost:4173/`.

The build also writes `sitemap.xml`, `robots.txt`, `llms.txt` and the
JSON-LD block on the home page. All of them take the site address from the
`canonical` value of `index.html` in `pages/pages.json`, so a domain change
means editing that file and the per-page canonicals next to it. Page JSON-LD
lives next to the page as `pages/<name>.jsonld` and `llms.txt` is a template
that uses `{{SITE}}` for the address.

## Demo video

`media/demo.mp4` and its poster `media/demo-poster.jpg`
are copied to the site as they are. The home page embeds the video with
controls and no autoplay.

## Deploy

A GitHub Pages workflow in `.github/workflows/pages.yml` builds this
directory and deploys it with `actions/upload-pages-artifact` and
`actions/deploy-pages`. It runs on a push to `main` that touches
`website/`, `web/docu/cli-commands.html` or the workflow file, and on
`workflow_dispatch`.

The custom domain `kafmirror.scalytics.io` is set in Settings, Pages, with a
CNAME record to `scalytics.github.io`.

The first deploy needs the repository Pages source set to GitHub Actions
in Settings, Pages. Do not delete the `gh-pages` branch until that
deploy has been checked.

## Fonts

Latin woff2 files for IBM Plex Sans, IBM Plex Mono and Space Grotesk
live in `fonts/`, with their SIL Open Font License 1.1 files. The
site loads them from this directory only. It does not request fonts
from a third-party host. System fonts stay in the fallback stack.

