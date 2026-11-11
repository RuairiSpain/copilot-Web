# Teams-Voice-Meeting

A Python service that lets an Azure AI Foundry voice agent join a Microsoft Teams meeting as a
participant. The agent listens and speaks. It uses Teams Phone extensibility and Azure
Communication Services (ACS), and the `azure-ai-projects` SDK (2.7.0 or later, voice agents preview).

## How it works

```
Teams meeting --dials--> Teams Phone number (resource account)
        --> ACS  --IncomingCall event--> Event Grid --> this service (/api/events)
        --> Foundry voice agent (answers, speech in/out, turn taking, interruptions)
```

1. Someone invites the service number to the Teams meeting.
2. ACS raises `Microsoft.Communication.IncomingCall`. Event Grid delivers it to `POST /api/events`.
3. At startup the service creates (or reuses) a Teams Phone extensibility **telephony binding** on the
   voice agent, using the resource account GUID and the ACS connection. Foundry returns an
   `incoming_call_url`, which the service logs and exposes at `GET /api/calls`.
4. Foundry answers the call through ACS and runs real-time speech recognition and synthesis, including
   turn taking and interruptions. The Python service does not handle audio.
5. The service finds the Foundry call record, polls it until it leaves `in_progress`, and records the
   end reason. It ends a call that exceeds `MAX_CALL_SECONDS`, and ends active calls on shutdown.

### Differences from the original plan

These come from the SDK models, not from preference:

* **Call connection ID.** The IncomingCall payload has `correlationId` and `serverCallId`. A call
  connection ID exists only after the call is answered, and Foundry answers it. The service logs and
  tracks `correlationId` instead.
* **Answering and binding.** The binding is created through
  `client.beta.voice_agents.telephony.create_binding`. Foundry owns the call session, so "connect the call
  to the agent" is the binding plus the Event Grid route, not code in this service.
* **Event routing.** Foundry generates the `incoming_call_url`. Two routes work:
  * **Direct (default):** point the Event Grid subscription at `incoming_call_url`. This service then only
    monitors calls (it still needs `/api/events`, or polls, to learn when a call starts).
  * **Relay:** point Event Grid at this service and set `FORWARD_INCOMING_CALLS=true`. The service
    forwards each event to `incoming_call_url` with a bearer token for `https://ai.azure.com/.default`.
    That authentication scheme is not documented in the SDK. **It is unverified**, so test it before relying on it.

## Infrastructure

1. Assign a Teams Phone service number to a **resource account**.
2. Create an Azure Communication Services resource.
3. Grant Event Grid write permission on the ACS resource.
4. Ask a tenant administrator to configure the Entra application and complete the app role assignments.

## Foundry configuration

1. In the Azure AI Foundry portal, create a **voice agent**. Note its name (`VOICE_AGENT_NAME`).
2. Create a project connection with category `AzureCommunicationServices`.
   Set the target to the ACS HTTPS endpoint and store the ACS resource ID in the metadata.
   Its name is `ACS_CONNECTION_NAME`.
3. Open **Channels > Phone numbers**, choose **Microsoft Teams**, and select secure incoming call delivery.
4. Enter the resource account object ID as a GUID, without the `28:orgid:` prefix
   (the service strips the prefix if you include it). Steps 3 and 4 are done by the service at startup;
   do them in the portal instead if you prefer.

## Run locally

```bash
cd Teams-Voice-Meeting
python -m venv .venv && source .venv/bin/activate
python -m pip install "azure-ai-projects>=2.7.0"   # or: pip install -e ".[dev]"
pip install -r requirements.txt
cp .env.example .env   # fill it in, then: set -a; source .env; set +a
az login               # DefaultAzureCredential uses your Azure CLI sign-in locally
python -m teams_voice_meeting
```

The service listens on port 8000. Expose it over HTTPS (for example with a dev tunnel) and create an
Event Grid subscription on the ACS resource for `Microsoft.Communication.IncomingCall`. The endpoint
answers the Event Grid validation handshake.

Endpoints: `POST /api/events`, `GET /api/calls`, `GET /healthz`.

## Use

Invite the service number to a Teams meeting. The call appears in the logs and at `/api/calls`.

## Tests

```bash
pip install -e ".[dev]" && pytest
```

Tests use fakes for the Foundry client. They do not call Azure.

## Limits

* Voice agents in `azure-ai-projects` are a preview feature (`allow_preview=True`). Names and behavior may change.
* The call monitor matches the Foundry call to an event by start time, because the SDK call record does not
  expose the ACS correlation ID. Simultaneous call starts can be matched in the wrong order. Tracking is for
  logging and the time limit, so a mismatch does not affect the call itself.
* Not tested against real Azure resources.
