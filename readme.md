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
Requires golang 1.21 or later

<b>Windows</b>: mingw, gcc, libvips, libglib

<b>Debian/Ubuntu</b>: libvips<br>
install golang manually to get the latest versions which will help performance
   ```
   sudo apt install golang-go libvips libvips-dev
   ``` 
   

<b>Arch:</b> libvips
```
sudo Pacman -S go libvips
sudo setcap CAP_NET_BIND_SERVICE=+eip /path/to/OnlySats
```

On windows: run `build.bat` Modify the batch script if you would like to switch modes.<br>
On linux: run `sh build.sh mode` Three modes are currently available, [release, experimental, debug]

### Configuration Files

**`config.toml`** is where you will find the server settings.

The TOML file is in progress, some of these settings may not affect anything. The settings you will find here will eventually be fully controllable through the hosted site on the admin page.<br>Defaults & explanations:

```toml
// https server settings
[server]
port = ":1500" //port, include colon
host = "localhost" //listening host, localhost works as far as I am aware
session_secret = "your-secret-key" //Deprecated, will be re-introduced. Session encraption key. OnlySats now uses temporary generated keys located in the data directory
read_timeout = 30 //sqlite read timeout in seconds 
write_timeout = 30 //sqlite write timeout in seconds

[database]
max_open_conns = 1 //Default, unused
max_idle_conns = 1 //Default, unused
conn_max_lifetime = 0 //Default, unused
cache_size = 10000 //Default, unused

[paths] //relative or absolute paths to storage directories. 
data_dir = "data" //where to store databases & keys
live_output_dir = "live_output" //satdumps live_output folder FILES IN THIS DIRECTORY MAY BE EXPOSED TO ANYONE THAT VISITS THE SITE
thumbnail_dir = "" //where to store generated thumbnails, reccomended: leave blank to have them stored with the original images
log_dir = "logs" //where to store logs, partially used

[thumbgen] //Thumbnail settings, adjust if it takes a long time to generate thumbnails for images or to increase quality.
max_workers = 4 //threads, increase if your have more threads available and thumbgen is running slowly. affects CPU usage
batch_size = 1000 //how many images to process per thread, affects MEMORY usage
thumbnail_width = 200 //width of generated thumbnails in px. Note: gallery thumbnails are in 200px wide canvases.
quality = 75 // 0-100 quality rating of the thumbnail, lower to increase performance, raise to increase quality
//width and quality will mainly affect STORAGE and NETWORK usage, but may impact CPU/MEM slightly when generating thumbnails.

[logging] //Partially used, 
level = "" //if set to "detailed" it will log thumbgen stats
file = "app.log" //unused maybe?? will be changing soon.
max_size = 100 //Unused. Maximum size in lines for any given log
max_backups = 3 //Unused? How many old logs to keep before deleting
max_age = 28 //Unused. How old a log can be before deleting
compress = true //Deprecated.
```


## Troubleshooting

### Common Issues

1. **CGO Errors, Issues Building**: Use the build scripts, they help ;3
2. **Database Locked**: Check for other processes using the database
3. **Permission Errors**: Give write permissions for data directories and socket permissions for port 80
4. **Memory or CPU Issues**: Reduce batch size or worker count for larger live_output folders
