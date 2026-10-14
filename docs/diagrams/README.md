# Diagrams

Each `.json` is an archify source, and the `.png` beside it is what the docs show.

To change one, edit the `.json`, check it with `archify validate <type> <file>.json --quality showcase`, then render the HTML with `archify deliver` and export the PNG at 3x. `architecture.json` is an `architecture` diagram and `connection.json` a `sequence`.
