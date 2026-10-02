# For admins: the Users page

Admins see a **Users** entry in the top bar. It lists every account and what
each one holds and has used, never anyone's text.

```figure
users
```

- Pick a period (A) to see runs, gateway calls, tokens and cost for the last 7,
  30 or 90 days or all time. Projects and chapters are what each account holds
  right now. The last row is the total.
- **Add an account** (B): a username (lower-case, 3 to 32 characters), an
  optional display name, a first password to pass on, and a role. The new
  account starts with its own settings, the five example writers and the three
  system agents. There is no sign-up form; this is the only way in.
- Each row has actions (C): **Edit** the display name or role, set a new
  **Password** (the account is signed out everywhere), **Disable** (it is signed
  out, its running work is cancelled, everything it owns is kept), **Enable**,
  and, for a disabled account, **Delete**, which removes the account and
  everything it owns after you type its username.

Two rules hold whatever anyone clicks: nobody changes their own role or
access, and one active admin always remains.

## First start and a lost admin password

The first account is created when the app starts for the first time, from the
username and password in the server's configuration, as an admin. If that
password is lost, the person running the server can set it again from the
configuration with the `-reset-admin-password` flag; see the README.
