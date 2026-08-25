# Name and license check for a new task manager

Date of all checks: 2026-08-25.

## Part A — Name candidates

### Method and limits

- Domain status comes from DNS lookup (`host <domain>`), checked 2026-08-25. A DNS answer means the domain is live. No DNS answer usually means the domain is free, but a registered domain can still show no DNS answer. Treat "no DNS answer" as a hint, not proof. Confirm with a registrar before you buy.
- GitHub counts come from the GitHub search API, checked 2026-08-25.
- Package registry status comes from direct HTTP checks against npm, PyPI, and crates.io, checked 2026-08-25.
- Trademark search: the USPTO search page at `https://tmsearch.uspto.gov/search/search-information?q=<name>` returns an empty shell with no result data over a plain page load; it needs the interactive web app. The EUIPO eSearch page has the same kind of interactive form. Mark trademark status **UNVERIFIED — manual check needed** for every candidate. Use these URLs by hand:
  - USPTO: `https://tmsearch.uspto.gov/search/search-information?q=<name>`
  - EUIPO: `https://euipo.europa.eu/eSearch/#basic/1+1+1+1/<name>`
- App store presence and "bad reading" checks come from web search result pages (DuckDuckGo Lite), checked 2026-08-25. A missing hit does not prove an app or a bad reading does not exist; it means none turned up in this pass.

### Domain status table (2026-08-25)

| Name | .app | .dev | .io | .ee |
|---|---|---|---|---|
| teha | free (NXDOMAIN) | free (NXDOMAIN) | **taken** | **taken** |
| toimeta | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) |
| kirjas | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) |
| toime | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) | **taken** |
| meeles | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) | **taken** |
| tehtud | **taken** | free (NXDOMAIN) | free (NXDOMAIN) | **taken** |
| korda | **taken** | **taken** | **taken** | **taken** |
| kava | **taken** | **taken** | **taken** | **taken** |
| siht | free (NXDOMAIN) | free (NXDOMAIN) | **taken** | **taken** |
| valmis | **taken** | free (NXDOMAIN) | UNVERIFIED (server error on lookup) | UNVERIFIED (no answer) |
| tegu | UNVERIFIED (server error on lookup) | UNVERIFIED (no answer) | **taken** | **taken** |
| teed | **taken** | **taken** | **taken** | **taken** |
| loend | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) | free (NXDOMAIN) |
| plaan | **taken** | free (NXDOMAIN) | **taken** | **taken** |

### GitHub, package registry, app store, and reading checks

| Name | GitHub top hit (stars) | GitHub total hits | npm / PyPI / crates.io | App store hit | English/other bad reading |
|---|---|---|---|---|---|
| teha | tehapo/tehapo-odometer (5) — unrelated widget; german company teha-wd.de (heating cost billing) | 459 | all free | none found | Urban Dictionary lists "teha" as a girl's name/nickname. No bad meaning. |
| toimeta | none | 0 | all free | none found | none found |
| kirjas | git-kirja (3) — unrelated; Kirjas Global Ltd. (consulting firm, active Facebook page) | 226 | all free | none found | none found |
| toime | toimer (9) — CLI timer, close but not identical | 38 | all free | none found | none found |
| meeles | Meeles (1) — unrelated | 24 | all free | "Meeles: Know what's cooking 24" — small Android food app (10+ installs) | none found |
| tehtud | Kahev6itlus (3) — unrelated | 192 | all free | none found | none found |
| korda | kordamp-gradle-plugins (141) — unrelated Gradle plugin family; Alexander Korda and Sebastian Korda are well-known people with this surname | 313 | all free | none found | one Urban Dictionary entry, "Korda Communications," defines it as dishonest talk. Weak signal, but worth noting. |
| kava | **Kava-Labs/kava (461)** — a well-known DeFi blockchain project | 5502 | **npm taken** (test framework), **PyPI taken** (pre-alpha project), crates.io free | **"Kava" coffee-review app** exists; kava.io already runs a blockchain product | Kava is also a well-known Pacific plant and drink name in English. Very strong prior meaning. |
| siht | SIHTC / sihttp / SiHTML (≤4 stars each) — unrelated | 272 | npm free, **PyPI taken**, crates.io free | none found | **Bad reading.** Urban Dictionary defines "siht" as a stand-in spelling for a common English vulgarity. Avoid. |
| valmis | **valmishq/valmis (108)** — an existing self-hosted, open-source AI agent platform, at `docs.valm.is`, in the same self-hosted OSS space | 128 | all free | "Ole valmis!" is an unrelated Estonian civil-defense app | none found, but the direct project-name collision is serious |
| tegu | **att/tegu (18)** — AT&T's SDN reservation manager (established open-source project) | 1419 | all free | **"Tegu: Servicios para el hogar"** — a live, active Android/iOS home-services marketplace app for Latin America | Tegu is also the common name of a large lizard, well known in the pet trade. |
| teed | **xavysp/TEED (267)** — a well-known edge-detection ML project | 739 | npm free, **PyPI taken**, crates.io free | **"Teed"** — a live golf-tracking app at teedapp.com | Slang dictionaries list "teed" (as in "teed off") for anger, and also for drunk/stoned. Mixed but present. |
| loend | diceware-ee (1) — unrelated | 17 | all free | none found | none found |
| plaan | plaan (dating app, techlabs-unitech), plaan (route-planning app, PitchWall) — low-star but overlapping product category | 84 | all free | none found | none found |

### Ranked recommendation

*Weighting note: the table below counts `.io` and `.ee` as heavily as `.app` and `.dev`. This project needs only `.app` and `.dev`, and it never sells a package on npm, PyPI or crates.io. Read the "risky" verdicts with that in mind: a taken `.io` is a small cost, a live software project or app with the same name is a real cost, and a bad reading in English is fatal.*

| Rank | Name | Verdict | Reason |
|---|---|---|---|
| 1 | **loend** | safe | All four domains free. No GitHub, npm, PyPI, or crates.io collision. No app store hit. No bad reading found. |
| 2 | **toimeta** | safe | All four domains free. Zero GitHub repository hits for the exact term. No package or app store collision. No bad reading found. |
| 3 | **kirjas** | safe | All four domains free. No package collision. One unrelated consulting firm uses the name, in a different field, with low visibility. |
| 4 | tehtud | safe, but confirm .app and .ee first | .dev and .io are free; .app and .ee are already in use by unrelated sites. No package or bad-reading issues. |
| 5 | toime | safe, but confirm .ee first | .app, .dev, .io free; .ee taken. Close to an existing small CLI project name ("toimer"), not identical. |
| 6 | meeles | risky | .ee domain taken. A small, low-traffic Android app already uses this exact name in a different category. |
| 7 | teha | risky | .io and .ee already in use. A German heating-billing company and an Urban Dictionary name entry both use the term, though in different fields. |
| 8 | korda | risky | All four domains taken. Weak but present negative slang association ("Korda Communications" = dishonest talk). |
| 9 | plaan | risky | .app and .io taken. Two existing apps in adjacent categories (dating, route planning) use the same name. |
| 10 | siht | taken/risky | .io and .ee domains taken. PyPI name taken. Confirmed bad reading: used online as a stand-in spelling for a common English vulgarity. |
| 11 | tegu | taken | .io and .ee domains taken. An established open-source project (AT&T's Tegu) and a live, active mobile app both use this exact name. Also a well-known lizard name in English. |
| 12 | teed | taken | All four domains taken. PyPI name taken. A live, established golf app already uses "Teed." Slang readings are mixed to negative. |
| 13 | valmis | taken | .app domain taken. A live, open-source, self-hosted AI agent platform already uses the name "Valmis," in a closely related space (self-hosted OSS tooling). Strong direct collision. |
| 14 | kava | taken | All four domains taken. npm and PyPI names taken. A major blockchain project (Kava Labs) and a coffee app both use the name. Kava is also a well-known plant and drink name in English, so it carries a strong prior meaning. |

---

## Part B — License and contribution policy

**Not legal advice.** This section is general information, not a legal opinion for a specific case. For a final decision, talk to a lawyer.

### 1. AGPL-3.0-or-later server with GPL-3.0-or-later clients, talking over HTTP only

This combination works. The AGPL and the GPL are compatible licenses; version 3 of each was written together so that code under one can combine with code under the other, per the [Free Software Foundation's GPL FAQ](https://www.gnu.org/licenses/gpl-faq.html) and the [AGPL FAQ entry on combining with GPLv3 code](https://www.gnu.org/licenses/gpl-faq.html#AGPLv3CorrespondingSource).

A client that only talks to the server over HTTP, with no shared code and no shared process, is a separate program under copyright law. The AGPL's network clause (section 13) applies only to the AGPL-covered program itself — the server. It does not reach out and cover a separate client program just because the client happens to send HTTP requests to that server. This point is explained in the [FSF's AGPL rationale document](https://www.gnu.org/licenses/why-affero-gpl.html) and in the [Software Freedom Law Center's AGPL compliance guide](https://softwarefreedom.org/resources/2012/AGPLv3-Compliance-Full-Text.html).

What a self-hoster must publish, in practice:
- **Server (AGPL-3.0-or-later):** if the self-hoster modifies the server and lets users interact with it over a network, section 13 requires the self-hoster to offer those users the modified server's Corresponding Source, at no charge, through a reasonably prominent link or notice in the running application. This applies even with no distribution of the binary at all — the network use itself triggers the duty. See the [GNU AGPL-3.0 license text, section 13](https://www.gnu.org/licenses/agpl-3.0.html#section13).
- **Client (GPL-3.0-or-later):** the ordinary GPL "conveying" rules apply. If the self-hoster distributes a modified client binary or app package to other people, the self-hoster must supply the Corresponding Source to those recipients. Running a client only for oneself creates no publishing duty, because the GPL does not have a network clause.

### 2. A later hosted (SaaS) version

Under a pure AGPL license, running the server as a hosted service still triggers section 13: every user of the hosted service must get an offer of the server's source, including any private modifications made to run the service. The AGPL does not let the operator keep server-side changes closed just because there is no distribution of a binary; the network-use trigger replaces the usual "distribution" trigger. See the [GNU AGPL-3.0 text](https://www.gnu.org/licenses/agpl-3.0.html#section13) and the [FSF AGPL rationale](https://www.gnu.org/licenses/why-affero-gpl.html).

**Sole-copyright-holder dual licensing:** copyright law lets the copyright holder license the same code under more than one license at the same time. If the author is the sole copyright holder of the codebase — meaning no outside contributor holds copyright in any part of it — the author can offer the AGPL version to the public for free, and separately sell a different (for example, a commercial, closed-terms) license to paying customers who do not want the AGPL's obligations. This is a common commercial open-source pattern; see the [Open Source Initiative's overview of dual licensing](https://opensource.org/faq#dual-license) and MariaDB's public explanation of its own [dual license structure](https://mariadb.com/bsl-faq-mariadb/) as a documented real-world example (MariaDB uses BSL, not AGPL, but the sole-rights-holder mechanism is the same idea).

**Why outside contributions complicate this:** the moment someone else contributes code and keeps their own copyright in it, the project has more than one copyright holder. The original author can then no longer re-license that contributor's code under a second, different license without the contributor's separate permission. Two tools fix this:
- A **Contributor License Agreement (CLA)** is a separate legal agreement a contributor signs, which grants the project owner (often broad, sometimes exclusive) rights to relicense the contributor's code, including under a future commercial license.
- A **Developer Certificate of Origin (DCO)** is lighter. A contributor does not sign a separate document; they add a `Signed-off-by` line to each commit, certifying they wrote the code (or otherwise have the right to submit it) under the project's open-source license. A DCO does not grant relicensing rights the way a CLA does — it only confirms the contribution is legitimately open source. The full text is at [developercertificate.org](https://developercertificate.org/). The Linux kernel project is a well-known large project that uses the DCO instead of a CLA; see the [Linux kernel documentation on Developer's Certificate of Origin](https://docs.kernel.org/process/submitting-patches.html#sign-your-work-the-developer-s-certificate-of-origin).

Practical result: if the author wants the freedom to dual-license later, a CLA (not just a DCO) is the tool that grants that freedom for contributions from other people. Without a CLA, the author can dual-license only the code they alone wrote.

### 3. GPL-family apps versus Google Play and the Apple App Store

**Google Play:** a GPL-3.0 app is allowed on Google Play. Google's developer terms do not forbid GPL code, and many GPL and LGPL apps ship there today.

**Apple App Store:** there is a real, well-documented conflict. The Apple Media Services (App Store) terms include DRM (FairPlay) and usage restrictions on how a downloaded binary can be copied and redistributed, layered on top of the app itself. The GPL (and GPLv2 in particular) requires that anyone who receives the binary also be free to copy and redistribute it under the GPL's own terms, with no added restriction. Apple's store terms add exactly such a restriction, which the FSF says conflicts with the GPL.

The best-documented real case is VLC: the VideoLAN team removed VLC from the iOS App Store in 2011 after a complaint that Apple's terms were incompatible with VLC's GPLv2 license, and it stayed off the store for about three years. See the [FSF's statement, "Correcting some misunderstandings about the last GPLv3 draft and Apple's App Store,"](https://www.fsf.org/blogs/licensing/more-about-the-app-store-gpl-enforcement) and reporting on the case, for example [Ars Technica's coverage of the VLC App Store removal](https://arstechnica.com/gadgets/2011/01/vlc-media-player-pulled-from-app-store-over-gpl-violation-claims/). VLC returned to the App Store in 2013 after VideoLAN obtained a special exception/relicensing arrangement for the iOS build; GPLv3 itself also added an explicit "Apple App Store" style exception process that some projects use, but this needs the copyright holder's own action — a plain GPL app is not automatically clear for the App Store.

### 4. AGPL server plus GPL Android client: signing key and reproducible builds

Neither the AGPL nor the GPL requires the release signing key itself to be published. The AGPL's Corresponding Source duty covers source code, build scripts, and the information needed to build and install a modified version — not private key material used to sign a release, which is not "source" under the license's own definition. See the [GNU GPLv3 text, definition of "Corresponding Source"](https://www.gnu.org/licenses/gpl-3.0.html#section1), which the AGPL incorporates.

**Reproducible builds** are a separate, voluntary quality practice, not a license requirement. F-Droid, the free-software Android app catalog, asks (but for most apps does not strictly require) that a build be reproducible, so that anyone can rebuild the published source and get a byte-identical APK, which proves the published binary matches the published source without needing the developer's private signing key at all — F-Droid signs verified builds with its own key. See the [F-Droid documentation on reproducible builds](https://f-droid.org/docs/Reproducible_Builds/) and the [Reproducible Builds project's own explanation of the concept](https://reproducible-builds.org/). Practical result for this project: AGPL plus GPL obligations end at publishing source; reproducible builds are a good, common practice for a security-conscious self-hosted project, worth adopting, but not a legal requirement of either license.

### 5. Copyright notice and trademark under an alias

**Copyright notice:** an alias (a pen name or handle) can validly appear in a copyright notice. Copyright law protects a work regardless of whether the author's real legal name is used; a notice such as "Copyright (C) 2026 <alias>" is a normal, common, and legally functional form. Publishing under a consistent, identifiable alias is standard practice for many open-source maintainers.

**Trademark:** here an alias is weaker. Trademark rights and registration generally rest on the applicant's legal identity or a registered legal entity, and enforcement (sending legal notices, defending the mark in court) is much harder to do anonymously. An alias-only registration can also create later problems proving ownership or standing in a dispute.

**Practical recommendation, not legal advice:**
- For the copyright notice on the code, an alias is fine: use the same alias consistently across every file and release, for example `Copyright (C) 2026 <alias>`, and, if wanted, name the project's copyright holder as "<alias> and contributors" once other people submit code.
- For a name and logo the author cares about protecting long-term, an alias-only trademark filing carries more risk than a copyright notice does. A person who wants strong trademark protection while keeping a low public profile commonly uses a separate legal entity (for example, a single-member company) as the registered trademark owner, with the alias staying attached only to the public-facing project and commit history. Get local legal advice before filing.
