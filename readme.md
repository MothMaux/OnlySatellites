# OnlySatellites Station Client

![OnlySats](./public/image/OnlySats_Logo.png "")

OnlySats is a website hosting app for satellite images captured by SatDump, with many integrated features for managing your ground station or server. 
<br><b>Current public features include:</b><br>
Image gallery with sorting, filtering, Collapsible passes, and thumbnails for reduced network usage<br>
Message board for announcements and alerts<br>
About page for the station<br>
<b>And features for the admin(s), including:</b>
Configuration for satellites and passes<br>
Aliasing for composite names and custom color schemes<br>
Webpage rate limiting<br>
Proxying Satdump's http server for embedded viewing<br>
Server hardware monitoring<br>
Manage users and privilages<br><br>
And much more to come


## Building and Running

### Building

Dependencies: 
Golang 1.21 or later is required for building.
Windows Run: libvips, libglib
Windows build: mingw, gcc, libvips, libglib
Linux build and run: vips
      `sudo apt install golang-go libvips libvips-dev`

### Configuration Files

- **`config.toml`**: Main application configuration file

The TOML configuration includes:

```toml
// https server settings
[server]
port = ":1500" //port, include colon
host = "localhost" //listening host, localhost works for everthing afaik
session_secret = "your-secret-key" //Deprecated, will be re-introduced. Session encraption key. OnlySats now uses temporary, randomly generated ENV VAR KEYs
read_timeout = 30 
write_timeout = 30

[database]
path = "data/image_metadata.db" //path to database file, deprecated. Now uses [paths] data_dir and .db names are hard-set
max_open_conns = 1 // database settings, 
max_idle_conns = 1
conn_max_lifetime = 0
cache_size = 10000

[paths] //relative or absolute paths to storage directories. 
data_dir = "data" //where to store databases
live_output_dir = "live_output" //satdumps live_output folder
thumbnail_dir = "" //where to store generated thumbnails, leave blank to have them stored with the original images
log_dir = "logs" //where to store logs

[thumbgen] //Thumbnail settings, adjust if it takes a long time to generate thumbnails for images or to increase quality.
max_workers = 4 //threads, increase if your have more threads available and thumbgen is running slowly.
batch_size = 1000 //how many processes to run at once
thumbnail_width = 200 //width of generated thumbnails in px. Note: gallery thumbnails are in 200px wide canvases.
quality = 75 // 0-100 quality rating of the thumbnail, lower to increase performance, raise to increase quality

[logging] //Logging level has no effect as of right now, logging features need to be improved. 
level = "info" //all of these will be removed eventually, and set in the webapp itself.
file = "app.log"
max_size = 100
max_backups = 3
max_age = 28
compress = true
```


## Troubleshooting

### Common Issues

1. **CGO Errors**: Ensure GCC is installed and CGO_ENABLED=1
2. **Database Locked**: Check for other processes using the database
3. **Permission Errors**: Ensure write permissions for data directories
4. **Memory Issues**: Reduce batch size or worker count for large datasets
