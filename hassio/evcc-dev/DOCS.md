# evcc (dev)

Custom Home Assistant add-on for this fork. It pulls `ghcr.io/diederik98/evcc:battery-planner` instead of the official `evcc/evcc` image.

Stop the official evcc add-on before starting this one. Both use host networking and the same ports.

Config and database paths match the official add-on (`/config/evcc.yaml`, `/data/evcc.db`). `/data` is per add-on slug, so copy `evcc.db` from the official add-on data folder or point `sqlite_file` at a shared path such as `/homeassistant/evcc.db`.

The GitHub Container image must be public. After the first GHCR publish, set package visibility to public if Home Assistant cannot pull it.
