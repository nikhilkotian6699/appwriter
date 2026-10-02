# The writers

Every voice in the Guild is a *writer*: a name, a description of the author
whose craft it should emulate, and a model behind it. You manage them on the
**Writers** page.

```figure
writers
```

Writers have roles (A): a **critic** can be convened on a chapter, a
**co-writer** can draft for you, and a writer can be both. Disabled writers (B)
stay in the list but are not asked to work.

## The cast

Five example writers come with every account, each emulating a famous author's
craft, never copying their words. Their descriptions are yours to edit.

| Writer | Looks for |
|---|---|
| **Hemingway** | Short declarative sentences, concrete nouns, strong verbs, dialogue that means more than it says. Hunts ornament, sentimentality and explanation that should have been cut. |
| **García Márquez** | The extraordinary told in a level voice, long sinuous sentences, sensory abundance. Looks for flat images and wonder that is over-explained. |
| **le Carré** | Moral ambiguity, institutions with their own weather, dialogue as fencing, restraint. Checks motive and plausibility, and whether every character wants something. |
| **Le Guin** | Societies with coherent rules, quiet lyrical prose, ethical weight without a sermon, worlds built from lived detail. Probes the logic of the world and exposition that should have been experience. |
| **Stephen King** | Plain-spoken narration, ordinary detail turned uncanny, scene-level suspense, voices you could pick out in a crowd. Flags slack pacing, vague threats and openings that clear their throat. |

Three *system agents* do fixed jobs and cannot be deleted, only edited:

- the **Editor-in-chief**, who merges the critics' notes into one list;
- the **Lead writer**, who applies the notes you accepted;
- the **Bible keeper**, who proposes updates to the story bible.

## Adding, editing and testing a writer

1. Click **Add writer** (C). Give it a name; a short *slug* and a model alias are
   filled in for you. The alias names the model on the gateway, and the dropdown
   lists the aliases your gateway knows; if it is not on the list, ask your
   admin.
2. Write the **system prompt**: the author whose craft the writer should
   emulate and what it should care about. The Guild adds its own fixed rules
   underneath every prompt: produce original text only, never reproduce passages
   from published work, always follow the required format.
3. Choose the roles, and set the *temperature* if you like: lower is steadier,
   higher is wilder.
4. Click **Test writer** (D) before saving. A short sample request goes through
   the gateway with this writer's alias and prompt, and the reply, or the error,
   appears right there. If the gateway does not know the alias, the test says so.
5. Save. **Duplicate** copies a writer to start a variation; **Enable** and
   **Disable** take a writer in and out of the Guild without deleting it.
