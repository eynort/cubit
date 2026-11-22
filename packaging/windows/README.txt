Cubit - a Minecraft bot that fights, digs and moves
https://github.com/eynort/cubit

Run it:

    cubit.exe --hub :8080 --open

That starts the panel and opens it in your browser. Keep the window open while
you use it (minimize it); close it to stop Cubit.

Where your data goes (bots, settings):
  - If a folder called "data" exists next to cubit.exe, Cubit keeps everything
    there (portable mode: create the folder to use it).
  - Otherwise, in the folder you start it from.
  - Or anywhere you like:  cubit.exe --hub :8080 --open --data C:\path\to\folder

Windows SmartScreen may warn about cubit.exe because it is not code-signed:
click "More info" and then "Run anyway". You can check the file against
SHA256SUMS on the release page.

Prefer a package manager?  scoop bucket add cubit https://github.com/eynort/cubit
                           scoop install cubit/cubit
