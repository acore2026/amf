Phase 1: Environment Setup & Baseline Validation
The easiest way to run Free5GC for development is using Docker Compose. You need to get the network running and connect a simulated user equipment (UE) to it.

Prompt for Claude Code:

"I want to run the free5gc project locally to test AMF modifications. Please search the web for the official free5gc-compose repository instructions. Guide me step-by-step on how to install the prerequisites (specifically the gtp5g kernel module), build the Docker images, and run a basic test with UERANSIM to ensure the AMF is working and a UE can successfully register. Do not write any Go code yet; focus purely on the infrastructure setup."

Phase 2: Codebase Reconnaissance
Once the baseline is running, you need Claude to explain the AMF structure to you in terms you already understand (3GPP protocols).

Prompt for Claude Code:

"Read the free5gc/amf directory structure and codebase. I am highly familiar with 3GPP AMF protocols but new to this specific Go project. Please map the 3GPP functions (e.g., NAS signaling decryption, N11 interface to SMF, N2 interface to gNB, Mobility Management) to the specific Go packages and files in this repository. Create a markdown file named AMF_ARCHITECTURE.md detailing this mapping to serve as our reference guide."

Phase 3: State Externalization (The Critical Hurdle)
Before you can split the logic into multiple tools, you must decouple the state. Currently, Free5GC stores UE context (AmfUe) in local memory. If you split the AMF into Tool A and Tool B, they will not share memory.

Prompt for Claude Code:

"Analyze how the AmfUe context and state are managed within the free5gc/amf/context package. Our goal is to externalize this state so multiple decoupled AMF tools can access it concurrently. Propose a step-by-step architectural plan to migrate this in-memory state to Redis (which Free5GC already uses for other functions). Detail how we will handle data locking and race conditions. Write this proposal to STATE_MIGRATION_PLAN.md. Ask for my approval before modifying any Go files."

Phase 4: Decouple the First Feature
Do not rewrite the whole AMF at once. Pick a specific, isolated Service Based Interface (SBI) to decouple first, such as Location Management or Event Exposure.

Prompt for Claude Code:

"Let us start the decoupling process by extracting the Namf_EventExposure service. I want to remove this logic from the main AMF monolith and create a separate, independent Go microservice (Tool) for it.

Create a new directory for this tool.

Move the relevant logic.

Define an internal gRPC interface for the main AMF to communicate with this new Event Exposure tool.
Please plan the code changes and show me the proposed file structure before executing the edits."

Phase 5: The Automated Test Loop
Tying back to your earlier idea, you will need to test continuously.

Prompt for Claude Code:

"I have written a script that triggers a UE registration test via UERANSIM. I want you to run this test script. If the test fails, analyze the AMF logs, identify the Go panic or routing error caused by our decoupling work, propose a fix, and apply it. Repeat this loop until the UE successfully registers."