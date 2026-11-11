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

## Deploy

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
* **Agent joins a scheduled meeting from a JSON file:** not built. The SDK has outbound call jobs
  (`telephony.create_call_job`, with a `schedule` and an ACS/Teams connection), but the destination must be an E.164
  phone number. It cannot join by meeting link. A Teams dial-in number asks for a conference ID, and the SDK shows
  no way to enter digits, so that is unconfirmed.

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
