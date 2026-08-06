# Model registry

No AI model is bundled or trusted implicitly. Before an artifact can be installed, add a manifest conforming to the public model schema, verify its code/model/data licenses, pin artifact hashes, record domain and safety evaluations, and move its status from `candidate` to `approved` through review.

The initial candidates are Qwen3 for the assistant, IndicTrans2 for Indian-language translation, and Whisper for speech recognition. They are intentionally not downloaded during the Phase 0 build; the deterministic order and KDS paths do not depend on AI availability.

