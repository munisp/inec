# AI Speech Writer Module: Technical Architecture and Capabilities
**Date:** July 17, 2026
**Author:** Manus AI

## Overview

The AI Speech Writer is a core innovation within the Candidate Campaign Planning microservice of the INEC platform. Designed to provide political candidates with instant, highly contextualized, and culturally resonant campaign materials, it leverages Large Language Models (LLMs) to generate dynamic speeches, manifestos, and press releases.

This report details the internal mechanics, prompting architecture, multilingual support system, and fail-safe mechanisms of the module.

## Core Architecture

The AI Speech Writer operates as an asynchronous Python function (`engine_speech`) exposed via a FastAPI REST endpoint (`/api/v1/campaign/speech`). It acts as an orchestration layer between the client application and an external OpenAI-compatible LLM provider.

### The Processing Pipeline

When a client submits a request, the pipeline executes the following sequence:

1.  **Parameter Extraction:** The system ingests the candidate's name, the specific office they are seeking (e.g., Gubernatorial, Senatorial), the state code, an array of key policy priorities, the desired language, and the target speech type.
2.  **Context Resolution:** The system maps the abbreviated state code (e.g., "LA") to the full state name ("Lagos") using the internal `STATES` reference dictionary. This ensures the LLM receives the correct geographic context. Similarly, the language code (e.g., "yo") is mapped to its full linguistic identifier ("Yoruba").
3.  **Dynamic Prompt Construction:** The engine selects a predefined prompt template based on the `speech_type`. It injects the resolved context variables (candidate name, office, state, policies, and language) into the template to create a highly specific instruction set for the LLM.
4.  **Asynchronous LLM Invocation:** Using the `httpx` library, the engine makes a non-blocking POST request to the configured `OPENAI_API_BASE` endpoint. It utilizes the `gpt-4o-mini` model (or the configured equivalent) to balance speed and output quality.
5.  **Response Parsing and Delivery:** The engine extracts the generated text from the LLM's JSON response payload, packages it with the original metadata, and returns it to the client along with a generation timestamp.

## Supported Speech Types

The module utilizes a specialized prompt dictionary to handle seven distinct campaign scenarios. Each prompt is engineered to enforce specific constraints regarding length, tone, and structure.

| Speech Type | Description | Tone/Style |
| :--- | :--- | :--- |
| `rally` | A 3-paragraph energetic campaign rally speech designed to energize a crowd. | Hopeful, patriotic, energetic |
| `manifesto` | A concise 5-point manifesto summarizing the candidate's core platform. | Structured, authoritative |
| `press_release` | A professional press release announcing the candidate's campaign launch. | Formal, journalistic |
| `debate_opening`| A 2-minute opening statement tailored for a televised debate format. | Persuasive, concise |
| `victory` | A gracious speech acknowledging a successful election outcome. | Unifying, grateful |
| `concession` | A dignified speech acknowledging electoral defeat and urging peace. | Respectful, calm |
| `policy_brief` | A 300-word deep dive into the candidate's top two policy priorities. | Analytical, detailed |

### Prompt Engineering Example

To illustrate the prompt construction, consider the `rally` template:

> "Write a 3-paragraph energetic campaign rally speech for {name} running for {office} in {state_name}, Nigeria. Key policies: {policies}. Language: {lang_name}. Tone: hopeful, patriotic."

This strict templating prevents the LLM from generating overly long or contextually inappropriate content.

## Multilingual Capabilities

A critical feature of the AI Speech Writer is its ability to generate content in Nigeria's major indigenous languages alongside English. This allows candidates to reach grassroots voters in their native tongue without requiring manual translation services.

The system currently supports:
*   **English (`en`)**: The default language for national communication and press releases.
*   **Hausa (`ha`)**: Essential for campaigns in the North-West and North-East geopolitical zones.
*   **Yoruba (`yo`)**: Crucial for voter mobilization in the South-West zone.
*   **Igbo (`ig`)**: Required for effective communication in the South-East zone.

### Implementation Mechanism

The language parameter is explicitly passed into the LLM prompt (e.g., `Language: Yoruba`). Because modern LLMs are trained on vast multilingual corpora, they are capable of directly generating the speech in the requested language while maintaining the specific nuances of the injected policies and the requested tone. This direct generation is generally superior to post-generation translation, as it preserves cultural idioms and rhetorical structures appropriate for political discourse in that specific language.

## Reliability and Fail-Safe Mechanisms

To ensure the service remains highly available even during API outages or network disruptions, the module implements a robust fallback mechanism.

If the `httpx` request to the LLM provider fails (due to a timeout, authentication error, or service unavailability), the exception is caught, logged via `structlog`, and the system immediately routes the request to the `_template_speech` fallback function.

The fallback function utilizes hardcoded Python f-strings to generate deterministic, template-based speeches. While these lack the dynamic flair and linguistic variety of the LLM output (and default to English), they guarantee that the client always receives a valid, structurally sound response containing their key policies and campaign details. This ensures zero downtime for the campaign planning dashboard.
