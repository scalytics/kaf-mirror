# kaf-mirror website

This directory is the source of the project site served at
<https://scalytics.github.io/kaf-mirror/>.

The pages are plain HTML and CSS, assembled by a short Python script
with no extra packages. `build.sh` writes the site to `_site/` and wraps
the generated CLI reference from `web/docu/cli-commands.html` with the same
header and footer.

```bash
./website/build.sh
python3 -m http.server --directory _site 4173
```

Then open `http://localhost:4173/`.

## Deploy

A GitHub Pages workflow in `.github/workflows/pages.yml` builds this
directory and deploys it with `actions/upload-pages-artifact` and
`actions/deploy-pages`. It runs on a push to `main` that touches
`website/`, `web/docu/cli-commands.html` or the workflow file, and on
`workflow_dispatch`.

The first deploy needs the repository Pages source set to GitHub Actions
in Settings, Pages. Do not delete the `gh-pages` branch until that
deploy has been checked.
