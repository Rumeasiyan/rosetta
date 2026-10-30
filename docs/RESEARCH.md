# Research: where we start

ROSETTA translates between **Sinhala, Tamil and English** for provincial systems. Machine output is always a draft for a person to check.

## What we need to find out

1. **Models and tools** that translate between the three languages and can run **on our own machines or on the device, with low resources** (no paid cloud service as the only option).
2. **Existing Sri Lankan work**, for example translation research from the University of Moratuwa: who built it, whether it is available, under what licence, whether it can be self-hosted, and how good it is.
3. **Quality:** how each option does on Sinhala to Tamil, Tamil to Sinhala, and each to and from English, on government-style text (letters, notices, forms).
4. **Resources:** memory, CPU or GPU, disk, and speed per sentence for each option.
5. **Glossaries:** how to keep one shared list of official terms so the same word is always translated the same way.
6. **Datasets** for testing, and their licences.

## How to record a finding

- **One finding, one issue**, using the **Research finding** template: what it is, link, licence, what it needs to run, what you tested and the result.
- **Longer write-ups** go in `docs/research/<topic>.md` through a pull request, with links to every source.
- **Test it if you can.** A short run on a few sentences, with the sentences and outputs, is worth more than a description.
- Put a link to anything you also saved in the shared Drive folder, but the finding itself must be in the repository to be counted.

## How we compare options

Use the same small test set for every option, so results can be compared. The first useful pull request is that test set: about 50 made-up sentences of government-style text in each language, in `docs/research/test-set/`.
