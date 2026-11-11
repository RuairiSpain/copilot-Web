# Teams-Voice-Meeting

An Azure AI Foundry **voice agent** that answers calls to a Microsoft Teams Phone number, through Teams Phone
extensibility and Azure Communication Services (ACS). Add the number to a Teams meeting as a phone participant
(or dial it) and the agent joins the call, listens and speaks.

Foundry owns the call. It answers through ACS, runs speech recognition and synthesis, and handles turn taking
and interruptions. There is **no runtime service to host**. This repo provides:

* `infra/deploy.sh` and `infra/main.bicep`: one script that deploys the whole estate.
* `teams-voice`: a small CLI (`azure-ai-projects>=2.7.0`) that creates the voice agent and Teams telephony
  binding, and lists, watches and ends calls.

```
Teams meeting / caller --> Teams Phone number (resource account) --> ACS --IncomingCall--> Event Grid
        --> Foundry binding (incoming_call_url) --> voice agent (media over ACS)
```

## Two join modes (switch with `JOIN_MODE`)

| | `invite` (default) | `calendar` |
| --- | --- | --- |
| How the agent gets into a meeting | A participant adds the agent's phone number to the call | The agent is on the invite; it reads its own Outlook calendar and joins at start time |
| Path | Teams Phone number, ACS, Foundry telephony binding | Microsoft Graph: calendar read, then `POST /communications/calls` |
| Voice agent speaks | Yes, Foundry handles the audio | **Not yet.** See "Calendar mode limit" |
| Network | Foundry private endpoint, no public endpoint of yours | One public HTTPS endpoint of yours for Graph call notifications |
| Deploy | `infra/deploy.sh` | `infra/deploy.sh --mode calendar` |
| Code | `foundry.py`, `consent.py`, `config.py` | `calendar_join/` |

The two modes share no code. A test checks that neither imports the other; only `cli.py` knows about both.
`teams-voice run` starts whichever mode `JOIN_MODE` names.

## Calendar mode

```bash
infra/deploy.sh --mode calendar --name tvm --agent-mailbox agent@contoso.com \
    --callback-url https://bot.contoso.com/api/calling
```

This creates an Entra app that requests the Graph application permissions `Calendars.Read` and `Calls.JoinGroupCall.All`,
an Azure Bot whose calling webhook is your callback URL, and `.env.calendar` (holds a client secret, git-ignored).
You host `teams-voice run` behind the callback URL; it listens on port 8080.

**Order matters.** `Calendars.Read` as an application permission can read every mailbox in the tenant. The script
therefore does **not** grant admin consent unless you pass `--grant-consent`. First an Exchange admin limits the app
to the agent mailbox (application access policy or RBAC for Applications) and sets the mailbox to auto-accept
invites. Only then should a tenant admin grant consent.

**Meetings from other tenants are skipped** (anyone who can invite the mailbox could otherwise send the agent into any
meeting). Set `ALLOW_EXTERNAL_MEETINGS=true` to allow them. The check reads the tenant ID from long join URLs; a short
`/meet/` URL carries none, so it cannot be checked.

What `teams-voice calendar run` does, every `POLL_SECONDS`:

1. `GET /users/{agent}/calendarView` for events starting within `JOIN_LEAD_SECONDS`, or already running.
2. Keeps Teams meetings that are not cancelled and not declined.
3. Parses the join URL (long `meetup-join` or short `/meet/<id>?p=` form, the two forms the Microsoft EchoBot sample accepts)
   and posts a join request to Graph.
4. Leaves `LEAVE_AFTER_END_SECONDS` after the scheduled end. State is kept in `.calendar-state.json`, so a restart
   neither rejoins nor forgets to leave.

Other commands: `teams-voice calendar list` shows the meetings it sees; `teams-voice calendar join-url <url>` joins one now.

**Calendar mode limit.** The join uses Graph service-hosted media, so the agent appears in the meeting but has no live audio
stream to Foundry. Live audio needs application-hosted media: Microsoft's media SDK (.NET, and in the EchoBot sample
Windows with a public IP, ports and a certificate) bridging 16 kHz PCM audio to a voice model. That bridge is not built.
Until it is, use calendar mode to prove the scheduling and join, or use `invite` mode for a working voice.

**Unverified in calendar mode:** the organizer-meeting join body (built the way the EchoBot sample does), whether the
callback must authenticate Graph's notifications (this MVP does not authenticate them and only logs them), the
permission set a real tenant needs, and Entra Agent ID mailboxes (any mailbox you can read with `Calendars.Read` works).

## Deploy (invite mode)

Needs `az` (logged in), `python3` 3.10+, rights to create resources, role assignments and Entra app registrations.

```bash
# 1. Infrastructure, Entra app, bot. Writes infra/out/teams-admin.ps1 and stops.
infra/deploy.sh --name tvm --location westeurope

# 2. A Teams admin runs infra/out/teams-admin.ps1, licenses the resource account
#    and assigns it a service number (admin centers). Note the resource account ObjectId.

# 3. Same script again: voice agent, telephony binding, Event Grid route, ACS consent.
infra/deploy.sh --name tvm --location westeurope --resource-account-id <ObjectId> --phone +353XXXXXXXXX
```

Re-running is safe. Add `--lockdown` to switch the Foundry public endpoint off (see Networking).
Other options: `--subscription`, `--acs-data-location` (must match the Teams tenant data location),
`--agent-name`.

Step 2 cannot be scripted from Azure: it needs a Teams admin, a Teams Phone license and a number.

## What gets created

| Resource | Purpose |
| --- | --- |
| Foundry account (AIServices) and project | Hosts the voice agent. Local auth disabled, Entra only. |
| Voice agent (CLI `setup`) | `VoiceAgentDefinition`, managed `gpt-realtime` by default. |
| ACS resource | Teams/PSTN call signalling and media. |
| Project connection `acs` | Category `AzureCommunicationServices`, target is the ACS endpoint, ResourceId in metadata. |
| Entra app (single tenant) and Azure Bot | The bot's Teams calling channel; the app ID is also the resource account's application ID. |
| VNet, NSG, private endpoint, 3 private DNS zones | Private path to Foundry. |
| Event Grid system topic and `incoming-call` subscription | Routes `Microsoft.Communication.IncomingCall` to the binding's `incoming_call_url`. |
| ACS server consent | Without it, calls commonly return Busy. |
| Azure AI User role for the person who deploys | Data-plane rights for the CLI (Owner does not include them). |

## Networking

Most of this estate cannot be private, and the template says so rather than pretending:

| Component | Exposure |
| --- | --- |
| ACS | Public. ACS has no private endpoint support. Teams and PSTN reach it from the internet. |
| Azure Bot (Teams channel) | Public by design. Its webhook is `https://eventgrid.azure.net`. |
| Event Grid to Foundry | Event Grid calls the binding's `incoming_call_url` on the Foundry endpoint. |
| Foundry | Private endpoint and DNS zones exist. The public endpoint is **on by default** so incoming calls work. |

`--lockdown` sets Foundry `publicNetworkAccess` to Disabled. **Unverified:** whether Event Grid delivery and the
ACS media bridge still work with it off. Test a call right after, and re-run without `--lockdown` if it fails.
With it off, run the CLI from inside the VNet (a VM, Bastion or a peered network).

## Wake and sleep words

The agent starts every call silent (there is no greeting). It speaks only after it hears a wake word, and
goes silent again, until the next wake word, when it hears a sleep word. Defaults: wake `hey assistant`,
sleep `go quiet`. Change them with comma-separated environment variables before running `deploy.sh` or `setup`:

```bash
WAKE_WORDS="hey bob,ok bob" SLEEP_WORDS="bob stop" teams-voice setup --update-agent
```

Set `WAKE_WORDS=` (empty) to turn trigger mode off. After changing words, publish a new agent version with
`--update-agent` (`deploy.sh` accepts it too).

**Limit:** this is done in the agent's instructions, because Foundry runs the call and the managed realtime model
decides when to speak. It is not a hard gate. The model can occasionally answer when it should stay silent, or
miss a wake word that was transcribed badly. A guaranteed gate needs code between the transcript and the voice,
which means a hosted-agent conversation engine (text in, text out, empty text while asleep). That is a larger build.

## Scheduled and invited calls

* **Someone in a Teams call invites the agent:** supported in principle. Add the resource account's phone number as a
  participant in the call or meeting; it arrives as an ordinary incoming call. Needs the inviter to be allowed to
  dial out to phone numbers. Not documented by Microsoft for this setup, so test it.
* **Agent joins a scheduled meeting on its own:** see "Calendar mode" above (Graph, agent's Outlook calendar). It joins, but live
  voice is not connected yet. A JSON-file schedule is not built. The SDK also has outbound call jobs
  (`telephony.create_call_job`, with a `schedule`), but the destination must be an E.164 number, so it cannot join by link,
  and a Teams dial-in number asks for a conference ID that the SDK shows no way to enter.

## CLI

```bash
set -a; . .env; set +a          # written by deploy.sh
teams-voice calls [--all]       # list calls
teams-voice watch               # log call start/end; ends calls over MAX_CALL_SECONDS (default 7200)
teams-voice end <call-id>
```

## Tests

```bash
pip install -e ".[dev]" && pytest     # fakes for Foundry, no Azure calls
```

## Unverified (check on the first real run)

* **Teams meeting use.** Microsoft documents calling the resource account number directly. Adding the number
  to a meeting as a phone participant dials it, so it should arrive as an ordinary incoming call. Not documented.
* **Event Grid route.** The portal's "Automatic configuration" (Channels > Phone numbers > Microsoft Teams) sets up
  incoming-call delivery itself. `deploy.sh` instead creates the subscription to `incoming_call_url` with `az`.
  Foundry's docs do not describe that exact subscription. If it fails or duplicates, use the portal option.
* **Project connection.** Bicep sets `authType: ApiKey` with the ACS key. Compare it with a connection made in the portal
  (`az rest --method get` on the connection) if `setup` rejects it.
* **Bot and resource account.** The app has no client secret. Microsoft's guide creates one; add it only if the bot needs it.
* **Preview API.** Voice agents use `allow_preview=True` and may change.
* Nothing here has run against a real subscription. The Bicep compiles; the script ran against a stub `az`.

## Troubleshooting

* `setup` fails with 401/403 right after deploy: the Azure AI User role is still propagating. Re-run.
* Calls return Busy: ACS server consent is missing. `teams-voice consent` (needs `ACS_ACCESS_KEY` in the environment).
* No incoming call reaches Foundry: check the Event Grid subscription's delivery status, and that the resource account
  has a service number and the Phone Resource Account license.
