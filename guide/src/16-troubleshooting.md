# Troubleshooting

**I cannot sign in.** Usernames are all lower-case; check the caps lock. After
five wrong passwords the app makes you wait a minute, then longer; the message
says how long. If you have forgotten the password, your admin can set a new one.

**The page says "sign in to continue" although I was signed in.** Your session
ended: you changed your password in another browser, an admin set a new
password or disabled your account, or the server restarted without a saved
session secret. Sign in again.

**The editor says "Unsaved changes" or "Could not save".** The server could not
be reached. Check your connection, then click **Save now**. Do not close the tab
until it says *Saved*. If it says *Conflict*, the chapter was changed in another
tab or browser; copy anything you want to keep, then click **Reload server
version**.

**Convene the Guild is greyed out.** A session is already running on this
chapter; wait for it, or cancel it from the Guild panel.

**A critic's card says the gateway does not know the alias.** The writer's
model alias does not exist on the gateway. Open the writer on the Writers page,
pick an alias from the dropdown, and click **Test writer**. If the dropdown is
empty, the gateway is not reachable; tell your admin.

**A critic failed with "rate-limiting" or an upstream error.** The gateway or
the model behind it was busy. The app already retried with pauses. The other
critics' notes are still there; convene again later for the one that failed.

**The editor-in-chief's list says it was assembled without the editor.** The
editor-in-chief failed or answered badly twice, so the critics' notes are listed
unmerged, most severe first. You can still decide on them. Check the
editor-in-chief's alias on the Writers page if it keeps happening.

**Revise is greyed out.** Accept at least one note first.

**The revision says it is stale.** You edited the chapter after the revision
was proposed. Discard it and click **Revise** again.

**A note's passage "is no longer in the chapter".** You changed that text since
the critique. The note is still shown; a revision simply skips it.

**A draft came back as a summary or with a chatty introduction.** The app strips
code fences and short lead-in lines automatically. If the text is still not
usable, discard it and ask again with a clearer instruction.

**Costs are marked "est.".** Streamed replies carry no exact cost, so the app
estimates from token counts and the gateway's prices. Expect the numbers to be
close, not exact.

**I am an admin and cannot change my own role, or disable myself.** By design.
Ask another admin, and there must always be at least one active admin.

**Nothing of mine is visible to the admin, is that right?** Yes. Admins see
accounts and usage, never anyone's text.
