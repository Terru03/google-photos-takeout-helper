import http.server
import socketserver
import time
import re
import sys

PORT = 8765

# Each fake file is 200 MB.
SIZE = 200 * 1024 * 1024

# Slow enough that you can see Chrome pause/resume behaviour.
CHUNK = 256 * 1024
DELAY = 0.25

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        print("%s - %s" % (self.address_string(), format % args))

    def do_GET(self):
        if self.path == "/":
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()

            links = ""
            for i in range(1, 9):
                links += '<p><a href="/takeout-test-{0:03}.zip">Download takeout-test-{0:03}.zip</a></p>'.format(i)

            html = """
<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Fake Takeout Test Downloads</title>
  <style>
    body {{ font-family: Arial, sans-serif; padding: 30px; }}
    a {{ font-size: 18px; }}
  </style>
</head>
<body>
  <h1>Fake Takeout Test Downloads</h1>
  <p>Click all 8 links. The test extension should keep only 2 running.</p>
  {links}
</body>
</html>
""".format(links=links)

            self.wfile.write(html.encode("utf-8"))
            return

        m = re.match(r"/(takeout-test-\d+\.zip)", self.path)
        if not m:
            self.send_error(404)
            return

        filename = m.group(1)
        start, end = 0, SIZE - 1
        range_header = self.headers.get("Range")

        if range_header:
            m2 = re.match(r"bytes=(\d+)-(\d*)", range_header)
            if m2:
                start = int(m2.group(1))
                if m2.group(2):
                    end = int(m2.group(2))

        length = end - start + 1

        self.send_response(206 if range_header else 200)
        self.send_header("Content-Type", "application/zip")
        self.send_header("Accept-Ranges", "bytes")
        self.send_header("Content-Disposition", 'attachment; filename="{}"'.format(filename))
        self.send_header("Content-Length", str(length))

        if range_header:
            self.send_header("Content-Range", "bytes {}-{}/{}".format(start, end, SIZE))

        self.end_headers()

        sent = 0
        try:
            while sent < length:
                block = min(CHUNK, length - sent)
                self.wfile.write(b"0" * block)
                self.wfile.flush()
                sent += block
                time.sleep(DELAY)
        except (BrokenPipeError, ConnectionResetError):
            print("Client disconnected while sending {}".format(filename))

def main():
    print("Open this in Chrome: http://127.0.0.1:{}".format(PORT))
    print("Leave this window open while testing.")
    print("Press Ctrl+C to stop.")
    with socketserver.ThreadingTCPServer(("127.0.0.1", PORT), Handler) as httpd:
        httpd.serve_forever()

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nServer stopped.")
        sys.exit(0)
