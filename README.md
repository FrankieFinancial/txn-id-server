# txn-id-server
Simple standalone service for generating a sequence of numbers. Can act in concert with other instances to form a cluster of independent units.

It's designed to be as minimalist as possible in terms of usage and resource consumption.
Simply open a TCP4 socket connection, read the number, terminated with a newline (0x0a) and the socket is closed automatically. No muss, no fuss. Guaranteed sequential numbers according to the options you specify (see below). 

Number generation has 2 parts to it. There's a uint16 (2 byte) component and a uint64 (8 byte) component, although we only use the upper 6 bytes of the latter. We increment the 2 byte part, starting at the number defined in the -instance option, adding -increment each time a new number is requested. When the uint16 overflows, we increment the 6 byte number. When returning a requested number we actually logically OR the 2 numbers to give a combined 64-bit unsigned integer.

*Why do we overcomplicate this?* To help make things safe across restarts.
Every time we increment the upper 6 bytes, we actually save the next number to a progress file (see below), so that when the server starts up, it can first read in that file and put the number into the upper 6 bytes, so that there is no way to repeat numbers across restarts (assuming all parameters remain the same). This does result in some numbers being "burnt" across restarts, but that is not the end of the world.

# Usage
```
./txnid [-daemon] [-ip n.n.n.n] [-port n] [-instance n] [-increment n] [-confdir /path/to/files] [-v]
```
By default, the service will run in the foreground, listening on port 0.0.0.0:8963. Numbers will farmed out, starting at 1 and incrementing by 1. Number generation is kept safe across restarts by saving the progress file in the confdir (default is /etc/txnidserver/txnid.dat) 

But as indicated above, all of those can be overridden by command line options. You can also set them using environment variables too. Order of precedence is:
* Environment variables are read first
* Commandline options override

## Parameters
* `-daemon (env: TXNID_SERVER_DAEMON=1):` will put the server into the background and exit with 0 if successful
* `-ip n.n.n.n (env: TXNID_SERVER_IP=n.n.n.n):`  listen only on the specified IP address or exits with a -1
* `-port n (env: TXNID_SERVER_PORT=n):` listen on the specified port. You obviously need appropriate permission for < 1024
* `-instance n (env: TXNID_SERVER_INSTANCE=n):` if you're running a number of instances as a "cluster", then what instance number is this? Can be 0-32767. It's also the starting number on the lower 2 bytes (as above)
* `-increment n (env: TXNID_SERVER_INCREMENT=n):` by default, the service increments by 1, but you have the option of incrementing by any number between 1 and 32767. If you're using a cluster (with -instance), then this should be set to the total number of instances you're running
* `-confdir /path/to/files (env: TXNID_SERVER_CONFDIR=/path/to/files):` the directory where the progress file is saved.
* `-v (env: TXNID_SERVER_VERBOSE=1):` by default the server runs silently, but if verbose is on, it will output what options it is using at the start (IP, port, etc) as well as a message whenever the lower 16 bits cycles over and the progress file is updated.

# Performance
It will merrily handle around 200000+ requests a second (tested on localhost on Ubuntu 18.04 on a Toshiba i7 laptop with 16GB of RAM) - you're largely limited by the hardware you run it on.

# TODO
* Convert generator into an embeddable package
* Allow a user:group to be set to chroot into if running in daemon mode
* Define a config file that can configure all of the above to be found in -confdir
* Create a separate long-lived port so clients can maintain a connection, rather than re-establish one each time.
** client would open a socket and send a `0x02` (STX - send transmission).
** server would respond with the number and a `0x03` (ETX - end transmission)
** client could send a `0x07` (bell - to go ping!) and expect a `0x07` in response to check a connection is alive
** server would time out after a configurable number of seconds of no traffic and close the socket
* Save all running params to the progress file too to assist in safer restarts - would override all other settings, commandline included
* Have a nice shutdown mode (`SIGTERM`) that writes the lower 16 byte number to the progress file to reduce the burn rate
