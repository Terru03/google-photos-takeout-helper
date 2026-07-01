TAKEOUT QUEUE PACKAGE

WHAT IS INSIDE

1) 01_TEST_EXTENSION_safe_fake_files
   Safe Chrome extension for testing only.
   It only manages files named takeout-test-...
   It keeps max 2 downloads active.

2) 02_REAL_EXTENSION_google_takeout
   Real Chrome extension for Google Takeout.
   It manages files/URLs containing takeout.
   It keeps max 5 downloads active.
   It retries interrupted downloads if Chrome says they are resumable.

3) 03_TEST_SERVER_fake_downloads
   Local fake download server.
   It creates 8 fake 200 MB files named takeout-test-001.zip etc.
   It supports resume/range requests.

TEST FIRST

1. Unzip this package.
2. Open Chrome.
3. Go to:
   chrome://extensions
4. Enable Developer mode.
5. Click Load unpacked.
6. Select:
   01_TEST_EXTENSION_safe_fake_files
7. Run:
   03_TEST_SERVER_fake_downloads\START_TEST_SERVER.bat
8. Open:
   http://127.0.0.1:8765
9. Click all 8 fake download links.

Expected result:
- only 2 fake downloads run at once
- the rest are paused
- when one finishes, another one resumes

REAL USE

1. In Chrome extensions, remove or disable the TEST extension.
2. Load unpacked:
   02_REAL_EXTENSION_google_takeout
3. Open Google Takeout.
4. Start the downloads manually.
5. The extension should keep only 5 active downloads.
6. If one fails and Chrome shows it as resumable, the extension should keep trying to resume it.

IMPORTANT

Do not run the TEST and REAL extensions at the same time.
Do not close Chrome while downloads are paused.
If a Google Takeout download fails with no Resume option, you must click that download manually again.
