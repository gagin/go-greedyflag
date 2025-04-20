==================================================
 go-greedyflag: Greedy Command-Line Flag Parsing for Go
==================================================

**Version:** |version|

.. |version| replace:: 0.1.0

.. placeholder for Go Report Card badge
.. placeholder for GoDoc badge
.. placeholder for License badge

``go-greedyflag`` provides an alternative command-line flag parsing library for Go, designed to feel familiar but offer enhanced capabilities, most notably "greedy" slice flags and configurable positional argument handling.

It addresses common frustrations with standard Go flag parsing, where flags like ``-e`` cannot intuitively consume multiple subsequent arguments (``mycmd -e go mod py``) without repetition or comma separation.

.. note::

  This library implements non-standard parsing behavior for greedy flags. While potentially more intuitive for certain use cases, it differs from POSIX/GNU conventions and standard Go libraries like ``flag`` and ``pflag``.

Features
--------

* **Greedy Slice Flags:** Define flags (e.g., ``-e``, ``--extensions``) that consume all subsequent non-flag arguments until another flag or ``--`` is encountered.
* **Standard Flags:** Supports standard boolean, string, int, etc., flags with short (``-f``) and long (``--flag``) names, compatible with ``pflag`` conventions (``StringVarP``, ``BoolVarP``, etc.). Handles ``-f=val``, ``--flag=val`` syntax for standard flags.
* **Configurable Positional Arguments:**
    * Default: No positional arguments allowed.
    * Mode A: Allow arbitrary positional arguments *only before* the first flag (``cmd pos1 pos2 --flag ...``).
    * Mode B: Require a mandatory number (N) of positional arguments, found either *before* the first flag OR at the *tail end* after all flags/arguments (``cmd pos1...posN -f ...`` OR ``cmd -f ... pos1...posN``).
* **``--`` Terminator:** Respects ``--`` to explicitly separate flags from positional arguments (relevant in Mode B).
* **Combined Short Flags:** Supports limited combination (e.g., ``-vb`` if ``-v`` is boolean), but value-requiring or greedy flags must be last.
* **Help Generation:** Automatic ``--help`` flag and customizable usage message.

Installation
------------

.. code-block:: bash

    go get github.com/gagin/go-greedyflag@latest # Or specific version when available

Basic Usage
-----------

.. code-block:: go

    package main

    import (
        "fmt"
        "os"

        "github.com/gagin/go-greedyflag" // Assuming this module path
    )

    func main() {
        // Define flags
        var extensions []string
        var files []string
        var verbose bool
        var output string

        // Define '-e' / '--ext' as a greedy string slice
        greedyflag.StringSliceGreedyVarP(&extensions, "ext", "e", []string{}, "Extensions to include (greedy)")
        // Define '-f' / '--file' as another greedy string slice
        greedyflag.StringSliceGreedyVarP(&files, "file", "f", []string{}, "Files to process (greedy)")
        // Define standard flags
        greedyflag.BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
        greedyflag.StringVarP(&output, "output", "o", "default.out", "Output file name")

        // Configure positional arguments (optional)
        // Example: Require exactly 2 mandatory leading-or-trailing positionals
        // greedyflag.SetMandatoryNArgs(2)
        // Example: Allow arbitrary leading positionals
        // greedyflag.AllowArbitraryLeadingPositionals()


        // Parse
        if err := greedyflag.Parse(); err != nil {
            fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
            // Usage is often printed automatically by pflag/greedyflag on error
            os.Exit(1)
        }

        // Use the flags
        if verbose {
            fmt.Println("Verbose mode enabled")
        }
        fmt.Printf("Output file: %s\n", output)
        fmt.Printf("Extensions (-e): %v\n", extensions)
        fmt.Printf("Files (-f): %v\n", files)
        fmt.Printf("Positional Args: %v\n", greedyflag.Args())

    }

Example Invocations
-------------------

.. code-block:: bash

    # Greedy flags consume until next flag
    ./mycmd -v -e go mod py -f main.go helper.go -o build.log

    # Using '=' with greedy flag consumes only that value
    ./mycmd -e=go -e py -f file1

    # Leading arbitrary positionals (if configured)
    ./mycmd file1 file2 -v -e go mod

    # Trailing mandatory positionals (if configured, N=2)
    ./mycmd -v -e go mod file1 file2

Limitations (Initial Version)
-----------------------------

* Does not handle subcommands.
* Greedy flags initially only support ``[]string``.
* Doesn't automatically handle shell glob expansion (relies on shell).
* **Multiple Greedy Flags:** If used consecutively (``-e val1 -f val2``), the first stops consuming when the second is encountered; the second becomes active.
* **Combined Short Flags:** Allowed (``-abc``) only if ``a`` and ``b`` are booleans. The last flag ``c`` can be any type. Value/greedy flags cannot appear before the end.
* **Positional Arguments:** Must be configured via API (`AllowArbitraryLeadingPositionals` or `SetMandatoryNArgs`). Default allows none. See Parsing Rules in SPEC.md for details.

Contributing
------------

(Add contribution guidelines here)

License
-------

MIT

