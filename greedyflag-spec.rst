==================================================
 Specification: greedyflag - Greedy Command-Line Flag Parsing for Go
==================================================

**Version:** 0.6 (Draft)

1. Introduction & Motivation
----------------------------

Go's standard ``flag`` package and popular alternatives like ``pflag`` adhere to POSIX/GNU-style parsing conventions. This library provides an alternative parsing mechanism featuring:

* **Greedy Slice Flags:** Allowing flags like ``-e`` to consume multiple subsequent non-flag arguments (e.g., ``mycmd -e go mod py``).
* **Configurable Positional Arguments:** Supporting either arbitrary positionals before flags or a mandatory number of positionals found either before flags or at the tail end.

2. Core Concept: Greedy Slice Flags
------------------------------------

* A **standard flag** consumes zero or one argument immediately following it or attached with ``=``.
* A **greedy slice flag** (associated with ``[]string`` etc.) consumes subsequent non-flag tokens when encountered without an ``=`` sign.
* Greedy consumption stops when another defined flag or the ``--`` terminator is encountered, or the argument list ends. The *last* greedy flag encountered becomes the active one.
* The scope of the active greedy flag ends as soon as another flag or ``--`` is encountered, or the end of the arguments is reached.
* If another flag is encountered, the parser processes that flag and potentially enters a new state.
* Positional arguments are handled according to the configured mode (see Rule 3 & API).

3. Parsing Rules
----------------

The parser's behavior depends heavily on the configured positional argument mode (Default: None; Mode A: Arbitrary Leading; Mode B: Mandatory N Leading-or-Trailing). Only one mode can be active.

1.  **Configuration Check:** Determine active positional mode (``None``, ``ArbitraryLeading``, ``MandatoryN``) and the value of ``N`` if applicable, based on API calls made before ``Parse()``. Initialize state variables: ``positionalsFound = []``, ``flagsSeen = false``, ``activeGreedyFlag = nil``, ``parsingComplete = false``.
2.  **Mandatory N - Leading Check (if Mode B active):**
    * Attempt to read the first ``N`` tokens from ``os.Args[1:]``.
    * If ``N`` non-flag tokens are found *before* encountering any token starting with ``-``:
        * Store these ``N`` tokens as the final positional arguments (``positionalsFound``).
        * Remove these ``N`` tokens from the list to be processed further.
        * Set ``flagsSeen = true`` (to prevent further leading positionals).
    * If a flag is encountered before finding ``N`` non-flag tokens, or if fewer than ``N`` tokens exist initially: Store any leading non-flags found (``k`` tokens where ``k < N``) temporarily. Proceed to flag parsing (step 3) with the full argument list (or remaining list). Remember ``k``.
3.  **Token Processing Loop (Flags & Greedy Args):** Iterate through the remaining command-line arguments. Maintain state.
    * If ``parsingComplete`` is true: Add token to a ``trailingArgsBuffer``. Continue loop.
    * If token is ``--``: Set ``parsingComplete = true``. Deactivate ``activeGreedyFlag``. Consume token. Continue loop.
    * If token looks like a flag (``-`` or ``--`` prefix):
        * Set ``flagsSeen = true``.
        * Process the flag (long, combined short, single short) based on its type (bool, standard value, greedy slice with ``=``, greedy slice without ``=``) according to rules from spec v0.4 (Rules 3, 4, 5). This includes activating/deactivating ``activeGreedyFlag``.
    * If token looks like a flag but is unknown: Report "unknown flag" error.
4.  **Non-Flag Token Encountered:**
    * If ``parsingComplete`` is true (after ``--``): Add token to ``trailingArgsBuffer``.
    * If ``activeGreedyFlag`` is set: Append token to the active greedy flag's slice.
    * If Mode A (``ArbitraryLeading``) is active **AND** ``flagsSeen`` is false: Append token to ``positionalsFound``.
    * If Mode B (``MandatoryN``) is active **AND** ``flagsSeen`` is true (i.e., after flags started): Buffer token in ``trailingArgsBuffer`` as a potential trailing positional.
    * Otherwise (e.g., Mode None, or Mode A after flags seen, or Mode B before flags seen but N already found/attempted): Report "unexpected argument" error.
5.  **End of Arguments:** Parsing loop finishes.
6.  **Positional Argument Validation:**
    * If Mode A (``ArbitraryLeading``) was active: ``greedyflag.Args()`` returns ``positionalsFound``. Check if ``trailingArgsBuffer`` is empty. If not, error ("non-flag arguments found after flags").
    * If Mode B (``MandatoryN(N)``) was active:
        * If ``N`` leading positionals were already found and stored in ``positionalsFound`` (Step 2a): Check if ``trailingArgsBuffer`` is empty. If not, error ("positional arguments found both before flags and at tail"). ``greedyflag.Args()`` returns ``positionalsFound``.
        * If fewer than ``N`` (or zero) leading positionals were found: Check if ``len(trailingArgsBuffer)`` equals ``N``. If yes, store ``trailingArgsBuffer`` contents as the final ``positionalsFound``. ``greedyflag.Args()`` returns ``positionalsFound``. If no (``len(trailingArgsBuffer) != N``), error ("expected N trailing arguments, found %d", len(trailingArgsBuffer)).
    * If Default Mode (None): Check if ``positionalsFound`` and ``trailingArgsBuffer`` are both empty. If not, error ("unexpected positional arguments found"). ``Args()`` returns empty slice.

4. API Design (Proposal)
-------------------------

The API operates on a default ``CommandSet``. Configuration functions must be called *before* ``Parse()``.

.. code-block:: go

    package greedyflag

    import "time"

    // --- Flag Definition ---
    // (Standard flag functions: StringP, StringVarP, IntP, BoolP, etc.)
    func StringP(name string, shorthand string, value string, usage string) *string
    func StringVarP(p *string, name string, shorthand string, value string, usage string)
    // ... other standard flags ...

    // (Greedy flag functions)
    func StringSliceGreedyP(name string, shorthand string, value []string, usage string) *[]string
    func StringSliceGreedyVarP(p *[]string, name string, shorthand string, value []string, usage string)

    // --- Parsing & Results ---
    func Parse() error // Returns error for parsing/validation issues
    func Args() []string // Returns positional arguments based on configured mode
    func NArg() int      // Returns number of identified positional arguments
    // (Visit, VisitAll, Lookup, Flag struct remain conceptually similar)

    // --- Configuration & Validation (Call BEFORE Parse) ---
    var Usage func()
    func PrintDefaults()

    // Configure positional argument handling mode (Mutually Exclusive)
    // Default: No positional arguments allowed (non-flag tokens are errors unless consumed by greedy flag).
    func AllowArbitraryLeadingPositionals() // Allow 0+ positionals ONLY before first flag.
    func SetMandatoryNArgs(n int)           // Require exactly N positionals, checking BEFORE flags first, then TAIL end.

    // Optional: SetErrorHandling(ErrorHandling)

5. Help Message (``--help``)
----------------------------

* The default ``Usage`` function should generate a help message.
* ``PrintDefaults()`` should list all flags.
* Usage string for **greedy slice flags** must clearly indicate behavior (e.g., ``<arg>...``). Examples:

  * ``-e, --extensions <ext>...      Extensions to include (greedy)``
  * ``-f, --files <path> [path...] Files to include manually (greedy)``

* Help text needs to explain greedy behavior, the effect of using ``=``, use of ``--``, rules for combined short flags, and the active positional argument mode.
* If positional argument requirements are set, the main usage line should reflect this:

  * Arbitrary Leading: ``Usage: mycmd [pos_args...] [flags]``
  * Mandatory N: ``Usage: mycmd <arg1>...<argN> [flags]`` or ``Usage: mycmd [flags] <arg1>...<argN>`` (clarify leading OR trailing).

6. Error Handling
-----------------

Report errors clearly, including:

* Unknown flag.
* Missing value for standard flag.
* Unexpected argument (based on configured positional mode).
* Type conversion errors.
* Invalid combined short flag syntax.
* Positional arguments provided when none are allowed.
* Wrong number of mandatory positional arguments found.
* Positional arguments found both before flags and at tail in Mandatory N mode.
* Calling both ``AllowArbitraryLeadingPositionals`` and ``SetMandatoryNArgs``.

7. Examples
-----------

* ``mycmd -v --logfile /tmp/log.txt -e go mod py -- main.go data/``
    * ``verbose``: true, ``logfile``: "/tmp/log.txt", ``extensions``: ``["go", "mod", "py"]``, ``Args()``: ``["main.go", "data/"]`` (Requires ``SetMandatoryNArgs(2)`` or similar allowing trailing after ``--``)
* ``mycmd --exclude *.tmp *.log -o output.txt``
    * ``exclude``: ``["*.tmp", "*.log"]``, ``output``: "output.txt", ``Args()``: ``[]``
* ``mycmd -e=go -e py -- file.txt``
    * ``extensions``: ``["go", "py"]``, ``Args()``: ``["file.txt"]`` (Requires ``SetMandatoryNArgs(1)`` or similar)
* ``mycmd -e go py -x *.tmp data -- file.txt``
    * ``extensions``: ``["go", "py"]``, ``exclude``: ``["*.tmp", "data"]``, ``Args()``: ``["file.txt"]`` (Requires ``SetMandatoryNArgs(1)`` or similar)
* ``mycmd -ve go mod`` (Assuming ``-v`` is ``BoolP``)
    * ``verbose``: true, ``extensions``: ``["go", "mod"]``, ``Args()``: ``[]``
* ``mycmd file1 file2 -v -e go mod`` (Requires ``AllowArbitraryLeadingPositionals()``)
    * ``Args()``: ``["file1", "file2"]``, ``verbose``: true, ``extensions``: ``["go", "mod"]``
* ``mycmd -v -e go mod file1 file2`` (Requires ``SetMandatoryNArgs(2)``)
    * ``verbose``: true, ``extensions``: ``["go", "mod"]``, ``Args()``: ``["file1", "file2"]``
* ``mycmd file1 file2 -v -e go mod`` (Requires ``SetMandatoryNArgs(2)``)
    * ``Args()``: ``["file1", "file2"]``, ``verbose``: true, ``extensions``: ``["go", "mod"]``
* ``mycmd -v file1 -e go mod file2`` (Requires ``SetMandatoryNArgs(2)``)
    * ``verbose``: true, ``extensions``: ``["go", "mod"]``, ``Args()``: ``["file1", "file2"]`` (Unintuitive case)
* ``mycmd file1 -v -e go mod`` (Requires ``SetMandatoryNArgs(2)``) -> Error: Found 1 leading arg, 0 trailing args, expected 2.
* ``mycmd -e go mod file1`` (Requires ``SetMandatoryNArgs(2)``) -> Error: Found 0 leading args, 1 trailing arg, expected 2.
* ``mycmd -e 1 2`` (Requires ``SetMandatoryNArgs(2)``) -> Error: Found 0 leading args, 0 trailing args (consumed by ``-e``), expected 2.
* ``mycmd -e go py file1.txt`` (Mode: Default or Arbitrary Leading) -> Error: unexpected argument ``file1.txt``.

8. Limitations / Non-Goals (Initial Version)
---------------------------------------------

* Does not handle subcommands.
* Greedy flags initially only support ``[]string``.
* Doesn't automatically handle shell glob expansion (relies on shell).
* **Multiple Greedy Flags:** If used consecutively (``-e val1 -f val2``), the first stops consuming when the second is encountered; the second becomes active according to its type.
* **Combined Short Flags:** Allowed (``-abc``) only if ``a`` and ``b`` are booleans. The last flag ``c`` can be any type. Value/greedy flags cannot appear before the end.
* **Positional Arguments:** Must be configured via API (`AllowArbitraryLeadingPositionals` or `SetMandatoryNArgs`). Default allows none. See Parsing Rules for details on placement and validation. The ``--`` terminator's role is primarily to stop flag parsing; subsequent tokens only contribute to trailing positional arguments if `SetMandatoryNArgs` is active.

