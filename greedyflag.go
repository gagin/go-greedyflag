// Package greedyflag provides a non-standard command-line flag parser
// supporting "greedy" slice flags and configurable positional arguments.
// WARNING: This library implements non-POSIX/GNU flag parsing conventions
// for greedy flags. Use with caution and clear documentation for users.
package greedyflag

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// --- Error Types ---
var (
	// ErrHelp is returned by Parse() if -h or --help was invoked.
	ErrHelp = errors.New("help requested")
	// ErrParsing indicates a syntax error during parsing.
	ErrParsing = errors.New("parsing error")
	// ErrValidation indicates an error after parsing (e.g., wrong positional count).
	ErrValidation = errors.New("validation error")
	// ErrConfiguration indicates conflicting API calls before Parse().
	ErrConfiguration = errors.New("configuration error")
)

// --- Value Interface (Similar to standard flag.Value) ---

// Value is the interface to the dynamic value stored in a flag.
// Set is called parsing flags, String is used for printing default values.
type Value interface {
	String() string
	Set(string) error
}

// --- Flag Struct ---

// Flag represents the state of a flag defined for the command line.
type Flag struct {
	Name      string // Long name of the flag.
	Shorthand string // Short name (one rune as string, empty if none).
	Usage     string // Help message.
	Value     Value  // Value instance associated with the flag.
	DefValue  string // Default value as text (used for help message).
	IsGreedy  bool   // Is this a greedy slice flag?
	IsBool    bool   // Is this a boolean flag (special parsing)?
	// Internal state
	changed bool // True if flag was set by the user on the command line.
}

// --- Concrete Value Types ---

// -- stringValue --
type stringValue string

func newStringValue(val string, p *string) *stringValue {
	*p = val
	return (*stringValue)(p)
}
func (s *stringValue) Set(val string) error {
	*s = stringValue(val)
	return nil
}
func (s *stringValue) String() string { return string(*s) }

// -- boolValue --
type boolValue bool

func newBoolValue(val bool, p *bool) *boolValue {
	*p = val
	return (*boolValue)(p)
}
func (b *boolValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		// Provide a more specific error than the default flag package might
		return fmt.Errorf("invalid boolean value %q", s)
	}
	*b = boolValue(v)
	return err
}
func (b *boolValue) String() string { return strconv.FormatBool(bool(*b)) }

// -- stringSliceValue -- (Used for Greedy Flags)
type stringSliceValue []string

func newStringSliceValue(val []string, p *[]string) *stringSliceValue {
	// Ensure the pointer is initialized if the default value is nil
	if val == nil {
		val = []string{}
	}
	*p = val
	return (*stringSliceValue)(p)
}
func (s *stringSliceValue) Set(val string) error {
	// For greedy flags, Set appends the raw argument.
	*s = append(*s, val)
	return nil
}
func (s *stringSliceValue) String() string {
	// Represent default value in help somewhat reasonably
	if len(*s) == 0 {
		return "[]"
	}
	// Quote elements containing spaces or commas? For now, just join.
	return "[" + strings.Join(*s, ",") + "]"
}

// --- Global State (Default Command Set) ---
var (
	flags                            = make(map[string]*Flag) // Map long name to Flag
	shortFlags                       = make(map[rune]*Flag)   // Map shorthand rune to Flag
	args                             = []string{}             // Stores final positional args found by Parse()
	parsed                           = false                  // Has Parse() been called?
	hasBeenConfigured                = false                  // Prevent config changes after first flag definition
	posMode           positionalMode = modeNone               // Default: no positionals
	mandatoryN        int            = -1                     // N for MandatoryN mode (-1 means not set)
	allowHelpFlag     bool           = true                   // Automatically handle -h/--help? (Can be disabled)
)

type positionalMode int

const (
	modeNone positionalMode = iota
	modeArbitraryLeading
	modeMandatoryN
)

// --- Flag Definition Functions ---

// AddFlag adds a flag definition to the default set. Internal use.
func addFlag(f *Flag) {
	if _, exists := flags[f.Name]; exists {
		// Use panic because this is a programmer error (defining flags twice)
		panic(fmt.Sprintf("greedyflag: flag redefined: %s", f.Name))
	}
	if f.Shorthand != "" {
		// Validate shorthand is single character
		if len(f.Shorthand) != 1 {
			panic(fmt.Sprintf("greedyflag: flag shorthand must be one character: %s", f.Shorthand))
		}
		// Get the rune for the map key
		shorthandRune, _ := utf8.DecodeRuneInString(f.Shorthand) // Get first rune
		if _, exists := shortFlags[shorthandRune]; exists {
			panic(fmt.Sprintf("greedyflag: flag shorthand redefined: -%s", f.Shorthand))
		}
		shortFlags[shorthandRune] = f
	}
	flags[f.Name] = f
	hasBeenConfigured = true // Lock positional config once flags are defined
}

// StringVarP defines a string flag with specified name, shorthand, default value, and usage string.
// The argument p points to a string variable in which to store the value of the flag.
func StringVarP(p *string, name string, shorthand string, value string, usage string) {
	addFlag(&Flag{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     newStringValue(value, p),
		DefValue:  value,
	})
}

// StringP is like StringVarP, but returns a pointer to a string variable.
func StringP(name string, shorthand string, value string, usage string) *string {
	p := new(string)
	StringVarP(p, name, shorthand, value, usage)
	return p
}

// BoolVarP defines a bool flag with specified name, shorthand, default value, and usage string.
// The argument p points to a bool variable in which to store the value of the flag.
func BoolVarP(p *bool, name string, shorthand string, value bool, usage string) {
	addFlag(&Flag{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     newBoolValue(value, p),
		DefValue:  strconv.FormatBool(value),
		IsBool:    true,
	})
}

// BoolP is like BoolVarP, but returns a pointer to a bool variable.
func BoolP(name string, shorthand string, value bool, usage string) *bool {
	p := new(bool)
	BoolVarP(p, name, shorthand, value, usage)
	return p
}

// StringSliceGreedyVarP defines a greedy []string flag with specified name, shorthand, default value, and usage string.
// The argument p points to a []string variable in which to store the values of the flag.
func StringSliceGreedyVarP(p *[]string, name string, shorthand string, value []string, usage string) {
	// Create a copy of the default value slice to avoid modification issues
	defaultValueCopy := make([]string, len(value))
	copy(defaultValueCopy, value)
	// Store default value representation for help message
	defValStr := newStringSliceValue(defaultValueCopy, new([]string)).String()

	addFlag(&Flag{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     newStringSliceValue(defaultValueCopy, p), // Use copy for Value too
		DefValue:  defValStr,
		IsGreedy:  true,
	})
}

// StringSliceGreedyP is like StringSliceGreedyVarP, but returns a pointer to a []string variable.
func StringSliceGreedyP(name string, shorthand string, value []string, usage string) *[]string {
	p := new([]string)
	StringSliceGreedyVarP(p, name, shorthand, value, usage)
	return p
}

// --- Positional Config Functions ---

// checkPositionalConfigConflict ensures only one positional mode is set before flags are defined.
func checkPositionalConfigConflict(newMode positionalMode) error {
	if hasBeenConfigured {
		return fmt.Errorf("%w: cannot change positional argument mode after flags have been defined", ErrConfiguration)
	}
	if posMode != modeNone && posMode != newMode {
		return fmt.Errorf("%w: cannot set multiple positional argument modes (current: %v, new: %v)", ErrConfiguration, posMode, newMode)
	}
	return nil
}

// AllowArbitraryLeadingPositionals configures the parser to accept zero or more
// positional arguments only before the first flag is encountered.
// This call is mutually exclusive with SetMandatoryNArgs. Must be called before defining flags or Parse.
func AllowArbitraryLeadingPositionals() error {
	if err := checkPositionalConfigConflict(modeArbitraryLeading); err != nil {
		return err
	}
	posMode = modeArbitraryLeading
	mandatoryN = -1 // Ensure N is not set
	slog.Debug("Positional mode set: Arbitrary Leading")
	return nil
}

// SetMandatoryNArgs configures the parser to require exactly N positional arguments.
// The parser first checks for N arguments before any flags. If not found, it checks
// for exactly N arguments at the tail end after all flags and flag arguments.
// This call is mutually exclusive with AllowArbitraryLeadingPositionals. Must be called before defining flags or Parse.
func SetMandatoryNArgs(n int) error {
	if n < 0 {
		return fmt.Errorf("%w: number of mandatory args cannot be negative", ErrConfiguration)
	}
	if err := checkPositionalConfigConflict(modeMandatoryN); err != nil {
		return err
	}
	posMode = modeMandatoryN
	mandatoryN = n
	slog.Debug("Positional mode set: Mandatory N", "N", n)
	return nil
}

// --- Parsing Function ---

// Parse parses the command-line arguments from os.Args[1:]. Must be called
// after all flags and positional requirements are defined and before flags are accessed.
// Returns ErrHelp if -h or --help was invoked, or another error if parsing/validation fails.
func Parse() error {
	if parsed {
		return fmt.Errorf("%w: Parse() already called", ErrParsing)
	}

	// Automatically add help flag if not disabled and not already defined
	if allowHelpFlag {
		if Lookup("help") == nil {
			// Use BoolVarP to potentially add -h as well, but avoid panic if -h exists
			helpPtr := new(bool)
			helpFlag := &Flag{
				Name:      "help",
				Shorthand: "", // Assume -h might be used by user
				Usage:     "Display this help message",
				Value:     newBoolValue(false, helpPtr),
				DefValue:  "false",
				IsBool:    true,
			}
			if _, exists := flags["help"]; !exists {
				flags["help"] = helpFlag
				// Only add -h if it's not already taken
				if _, exists := shortFlags['h']; !exists {
					helpFlag.Shorthand = "h"
					shortFlags['h'] = helpFlag
				}
			}
		}
	}

	osArgs := os.Args[1:]
	args = []string{} // Reset global args

	var leadingPositionals []string
	var trailingArgsBuffer []string
	var activeGreedyFlag *Flag = nil
	var flagsSeen bool = false

	// --- Pass 1 (Conceptual for MandatoryN Leading Check) ---
	foundLeadingMandatory := false
	leadingArgsToProcess := osArgs // Start with all args
	if posMode == modeMandatoryN && mandatoryN >= 0 {
		tempLeading := []string{}
		firstFlagIndex := -1
		for i, arg := range osArgs {
			// Basic check for potential flag start
			if strings.HasPrefix(arg, "-") && len(arg) > 1 && !isNumeric(arg) {
				firstFlagIndex = i
				break
			}
			tempLeading = append(tempLeading, arg)
		}

		// If exactly N args found before any flag OR if N args is all there is
		if len(tempLeading) == mandatoryN && (firstFlagIndex == mandatoryN || (firstFlagIndex == -1 && len(osArgs) == mandatoryN)) {
			slog.Debug("Found mandatory N leading positional arguments", "count", mandatoryN, "args", tempLeading)
			leadingPositionals = tempLeading           // Store them
			leadingArgsToProcess = osArgs[mandatoryN:] // Process flags after these
			foundLeadingMandatory = true
			flagsSeen = true // Act as if flags started
		} else {
			slog.Debug("Mandatory N leading positional arguments not found/matched", "needed", mandatoryN, "found_before_flag", len(tempLeading))
			// Will check trailing args later
		}
	}

	// --- Pass 2 (Main Parsing Loop) ---
	i := 0
	for i < len(leadingArgsToProcess) {
		arg := leadingArgsToProcess[i]
		i++ // Consume argument for next iteration by default

		slog.Debug("Parsing token", "token", arg, "index", i-1, "greedy_active", activeGreedyFlag != nil)

		// Handle terminator first
		if arg == "--" {
			slog.Debug("Parsing stopped by terminator '--'")
			parsingComplete = true
			activeGreedyFlag = nil
			// Buffer remaining args only if MandatoryN mode might need them
			if posMode == modeMandatoryN && !foundLeadingMandatory {
				trailingArgsBuffer = append(trailingArgsBuffer, leadingArgsToProcess[i:]...)
				slog.Debug("Buffering args after -- for potential trailing positionals", "buffered", trailingArgsBuffer)
			}
			break // Stop processing loop
		}

		// If a greedy flag is active, try to consume
		if activeGreedyFlag != nil {
			// Does current arg look like a flag? (Improved check)
			isPotentialFlag := strings.HasPrefix(arg, "-") && len(arg) > 1 && !isNumeric(arg)
			isPotentialLongFlag := strings.HasPrefix(arg, "--") && len(arg) > 2

			if isPotentialFlag || isPotentialLongFlag {
				slog.Debug("Greedy consumption stopped by potential flag", "arg", arg, "previous_greedy_flag", activeGreedyFlag.Name)
				activeGreedyFlag = nil // Stop greedy mode
				i--                    // Re-process this token as a potential flag
				continue
			} else {
				// Consume argument for the greedy flag
				slog.Debug("Consumed by greedy flag", "arg", arg, "greedy_flag", activeGreedyFlag.Name)
				if err := activeGreedyFlag.Value.Set(arg); err != nil {
					return fmt.Errorf("%w: error setting greedy flag '%s': %v", ErrParsing, activeGreedyFlag.Name, err)
				}
				activeGreedyFlag.changed = true
				continue // Move to next argument
			}
		}

		// If not consuming greedy, check if it's a flag
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			if !flagsSeen && posMode == modeArbitraryLeading {
				// If we were collecting leading positionals and hit the first flag
				slog.Debug("First flag encountered, stopping leading positional collection")
			}
			flagsSeen = true // Mark that we've encountered a flag

			// Handle long flags (--flag or --flag=value)
			if strings.HasPrefix(arg, "--") {
				name := arg[2:]
				value := ""
				hasValue := false
				if equals := strings.Index(name, "="); equals != -1 {
					value = name[equals+1:]
					name = name[:equals]
					hasValue = true
				}

				// Handle help flag explicitly
				if name == "help" && allowHelpFlag {
					return ErrHelp
				}

				f := Lookup(name)
				if f == nil {
					return fmt.Errorf("%w: unknown long flag --%s", ErrParsing, name)
				}

				f.changed = true
				activeGreedyFlag = nil // Deactivate previous greedy

				if f.IsBool {
					if hasValue {
						if err := f.Value.Set(value); err != nil {
							return fmt.Errorf("%w: invalid boolean value %q for flag --%s: %v", ErrParsing, value, name, err)
						}
					} else {
						if err := f.Value.Set("true"); err != nil {
							return fmt.Errorf("%w: internal error setting boolean flag --%s: %v", ErrParsing, name, err)
						}
					}
				} else if f.IsGreedy {
					if hasValue {
						if err := f.Value.Set(value); err != nil {
							return fmt.Errorf("%w: error setting greedy flag '%s' with '=': %v", ErrParsing, f.Name, err)
						}
					} else {
						activeGreedyFlag = f
						slog.Debug("Greedy mode activated", "flag", f.Name)
					}
				} else { // Standard flag expecting value
					if hasValue {
						if err := f.Value.Set(value); err != nil {
							return fmt.Errorf("%w: invalid value %q for flag --%s: %v", ErrParsing, value, name, err)
						}
					} else {
						if i >= len(leadingArgsToProcess) || (strings.HasPrefix(leadingArgsToProcess[i], "-") && !isNumeric(leadingArgsToProcess[i])) || leadingArgsToProcess[i] == "--" {
							return fmt.Errorf("%w: flag needs an argument: --%s", ErrParsing, name)
						}
						value = leadingArgsToProcess[i]
						i++ // Consume the value argument
						if err := f.Value.Set(value); err != nil {
							return fmt.Errorf("%w: invalid value %q for flag --%s: %v", ErrParsing, value, name, err)
						}
					}
				}
				continue // Move to next argument after processing flag
			}

			// Handle short flags (-f, -f=value, -fvalue, -abc)
			namePart := arg[1:]     // Part after '-'
			if len(namePart) == 0 { // Just "-"
				// Treat as potential trailing positional if mode B and flags seen, else error
				if posMode == modeMandatoryN && flagsSeen && !foundLeadingMandatory {
					trailingArgsBuffer = append(trailingArgsBuffer, arg)
					slog.Debug("Buffering potential trailing positional", "arg", arg)
				} else if posMode == modeArbitraryLeading && !flagsSeen {
					leadingPositionals = append(leadingPositionals, arg)
					slog.Debug("Collected leading positional", "arg", arg)
				} else {
					return fmt.Errorf("%w: unexpected argument '-'", ErrParsing)
				}
				continue
			}

			// Check for equals sign: -f=value
			if equals := strings.Index(namePart, "="); equals != -1 {
				shortName := namePart[:equals]
				value := ""
				if equals < len(namePart)-1 {
					value = namePart[equals+1:]
				}
				if len(shortName) != 1 {
					return fmt.Errorf("%w: invalid short flag format %s", ErrParsing, arg)
				}

				shorthandRune := rune(shortName[0])
				f := shortFlags[shorthandRune]
				if f == nil {
					return fmt.Errorf("%w: unknown short flag -%s", ErrParsing, shortName)
				}
				f.changed = true
				activeGreedyFlag = nil

				if f.IsBool {
					return fmt.Errorf("%w: boolean flag -%s cannot have value %q", ErrParsing, shortName, value)
				}
				if err := f.Value.Set(value); err != nil {
					return fmt.Errorf("%w: invalid value %q for flag -%s: %v", ErrParsing, value, shortName, err)
				}
				// Note: Greedy flags with '=' don't activate greedy mode
				continue
			}

			// Handle combined (-abc) or single (-f) or attached value (-fvalue)
			activeGreedyFlag = nil // Deactivate previous greedy before processing short flags
			for j, r := range namePart {
				isLastChar := (j == len(namePart)-1)
				f := shortFlags[r]
				if f == nil {
					// Check for help flag explicitly
					if namePart == "h" && allowHelpFlag {
						return ErrHelp
					}
					return fmt.Errorf("%w: unknown flag in short flags: -%c (in %s)", ErrParsing, r, arg)
				}
				f.changed = true

				if !isLastChar { // Characters before the last must be booleans
					if !f.IsBool {
						return fmt.Errorf("%w: flag -%c requires value, cannot be combined before end in %s", ErrParsing, r, arg)
					}
					if err := f.Value.Set("true"); err != nil {
						return fmt.Errorf("%w: internal error setting boolean flag -%c: %v", ErrParsing, r, err)
					}
				} else { // Last character in the group (or only character)
					if f.IsBool {
						if err := f.Value.Set("true"); err != nil {
							return fmt.Errorf("%w: internal error setting boolean flag -%c: %v", ErrParsing, r, err)
						}
					} else if f.IsGreedy {
						activeGreedyFlag = f // Activate greedy mode for subsequent args
						slog.Debug("Greedy mode activated", "flag", f.Name)
					} else { // Standard flag expecting value
						if i >= len(leadingArgsToProcess) || (strings.HasPrefix(leadingArgsToProcess[i], "-") && !isNumeric(leadingArgsToProcess[i])) || leadingArgsToProcess[i] == "--" {
							return fmt.Errorf("%w: flag needs an argument: -%c (in %s)", ErrParsing, r, arg)
						}
						value := leadingArgsToProcess[i]
						i++ // Consume value
						if err := f.Value.Set(value); err != nil {
							return fmt.Errorf("%w: invalid value %q for flag -%c: %v", ErrParsing, value, r, err)
						}
					}
				}
			}
			continue // Move to next argument after processing short flag(s)
		} // End flag handling

		// --- Handle Non-Flag Token ---
		if posMode == modeArbitraryLeading && !flagsSeen {
			leadingPositionals = append(leadingPositionals, arg)
			slog.Debug("Collected leading positional", "arg", arg)
		} else if posMode == modeMandatoryN && !foundLeadingMandatory {
			// Buffer non-flags seen after flags start if leading N weren't found
			trailingArgsBuffer = append(trailingArgsBuffer, arg)
			slog.Debug("Buffering potential trailing positional", "arg", arg)
		} else {
			// Error: Unexpected non-flag argument based on mode
			// (e.g., default mode, or arbitrary mode after flags seen, or mandatory N mode after leading found)
			return fmt.Errorf("%w: unexpected argument '%s'", ErrParsing, arg)
		}

	} // End argument loop

	// --- Final Positional Argument Validation ---
	parsed = true
	finalPositionals := []string{}

	switch posMode {
	case modeArbitraryLeading:
		if len(trailingArgsBuffer) > 0 {
			return fmt.Errorf("%w: non-flag arguments found after flags when arbitrary leading positionals expected: %v", ErrValidation, trailingArgsBuffer)
		}
		finalPositionals = leadingPositionals
		slog.Debug("Validation: Arbitrary Leading Positionals", "count", len(finalPositionals), "args", finalPositionals)

	case modeMandatoryN:
		if foundLeadingMandatory { // N args were found before flags
			if len(trailingArgsBuffer) > 0 {
				return fmt.Errorf("%w: non-flag arguments found after flags when %d leading positionals were already found", ErrValidation, mandatoryN)
			}
			finalPositionals = leadingPositionals // Use the ones found earlier
			slog.Debug("Validation: Mandatory N Leading Positionals", "required", mandatoryN, "found", len(finalPositionals), "args", finalPositionals)
			// Already checked count == mandatoryN when setting foundLeadingMandatory
		} else { // N args were NOT found before flags, check trailing buffer
			if len(trailingArgsBuffer) != mandatoryN {
				return fmt.Errorf("%w: expected exactly %d trailing positional arguments, found %d: %v", ErrValidation, mandatoryN, len(trailingArgsBuffer), trailingArgsBuffer)
			}
			finalPositionals = trailingArgsBuffer
			slog.Debug("Validation: Mandatory N Trailing Positionals", "required", mandatoryN, "found", len(finalPositionals), "args", finalPositionals)
		}

	case modeNone:
		// leadingPositionals should be empty by definition if modeNone
		if len(trailingArgsBuffer) > 0 {
			return fmt.Errorf("%w: positional arguments are not allowed: %v", ErrValidation, trailingArgsBuffer)
		}
		slog.Debug("Validation: No positional arguments allowed or found.")
	}

	// Assign final positionals to global state
	args = finalPositionals

	// Check if help was requested during parsing
	helpFlag := Lookup("help")
	if allowHelpFlag && helpFlag != nil && helpFlag.changed {
		return ErrHelp
	}

	return nil // Success
}

// --- Result Access Functions ---

// Args returns the non-flag command-line arguments based on the configured mode.
func Args() []string {
	if !parsed {
		fmt.Fprintln(os.Stderr, "Warning: Args() called before Parse()") // Or return error?
		return []string{}
	}
	// Return a copy to prevent modification? For now, return direct slice.
	return args
}

// NArg returns the number of non-flag command-line arguments found.
func NArg() int {
	if !parsed {
		fmt.Fprintln(os.Stderr, "Warning: NArg() called before Parse()")
		return 0
	}
	return len(args)
}

// Lookup returns the Flag structure for the defined flag name (long name).
func Lookup(name string) *Flag {
	return flags[name] // Returns nil if not found
}

// Visit visits the command-line flags that were set, calling fn for each.
// It visits only those flags specified on the command line.
func Visit(fn func(*Flag)) {
	if !parsed {
		fmt.Fprintln(os.Stderr, "Warning: Visit() called before Parse()")
		return
	}
	// Need deterministic order
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := flags[name]
		if f.changed {
			fn(f)
		}
	}
}

// VisitAll visits all defined command-line flags, calling fn for each.
func VisitAll(fn func(*Flag)) {
	// Need deterministic order
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fn(flags[name])
	}
}

// --- Help/Usage ---

// Usage can be overridden by the user. The default prints a usage message.
// It is called by Parse() upon error or when help is requested.
var Usage = defaultUsage

// defaultUsage prints a usage message documenting all defined command-line flags to os.Stderr.
func defaultUsage() {
	// Generate the top usage line based on configuration
	usageLine := fmt.Sprintf("Usage: %s", os.Args[0])
	hasFlags := len(flags) > 0
	posDesc := ""

	switch posMode {
	case modeArbitraryLeading:
		posDesc = "[pos_args...]"
		if hasFlags {
			usageLine += " " + posDesc + " [flags]"
		} else {
			usageLine += " " + posDesc
		}
	case modeMandatoryN:
		argsList := make([]string, mandatoryN)
		for i := 0; i < mandatoryN; i++ {
			argsList[i] = fmt.Sprintf("<arg%d>", i+1)
		}
		posDesc = strings.Join(argsList, " ")
		// Show both forms as possible usage patterns
		usageLine += fmt.Sprintf(" %s [flags]\n   or: %s [flags] %s", posDesc, os.Args[0], posDesc)
	case modeNone:
		if hasFlags {
			usageLine += " [flags]"
		}
	}
	fmt.Fprintln(os.Stderr, usageLine)

	// Print flag defaults
	PrintDefaults()
}

// PrintDefaults prints, to standard error, a usage message documenting all defined command-line flags.
func PrintDefaults() {
	fmt.Fprintf(os.Stderr, "\nFlags:\n")
	VisitAll(func(f *Flag) {
		line := "  "
		// Format short/long name part
		short := ""
		if f.Shorthand != "" {
			short = fmt.Sprintf("-%s", f.Shorthand)
		}
		long := fmt.Sprintf("--%s", f.Name)

		if short != "" {
			line += fmt.Sprintf("%s, %s", short, long)
		} else {
			// Pad for alignment if no short flag
			line += fmt.Sprintf("    %s", long)
		}

		// Add type/value indicator
		typeName, hasArgument := flagType(f)
		if hasArgument {
			if f.IsGreedy {
				line += fmt.Sprintf(" %s...", typeName) // Indicate greedy repetition
			} else {
				line += fmt.Sprintf(" %s", typeName)
			}
		}

		// Calculate padding for usage string alignment
		// This is tricky to get perfect; aim for reasonable alignment
		namePartLen := len(line)
		padding := ""
		if namePartLen < 24 { // Adjust this threshold as needed
			padding = strings.Repeat(" ", 24-namePartLen)
		} else {
			padding = "\n    \t" // Wrap if too long
		}
		line += padding

		// Add usage string, indenting subsequent lines
		line += strings.ReplaceAll(f.Usage, "\n", "\n    \t"+strings.Repeat(" ", 24)) // Keep indent consistent

		// Add default value if not boolean and has a non-zero default string
		if !f.IsBool && f.DefValue != "" && f.DefValue != "[]" && f.DefValue != "false" && f.DefValue != "0" {
			line += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		// Add greedy indicator (optional, already in type name)
		// if f.IsGreedy { line += " (greedy)" }

		fmt.Fprintln(os.Stderr, line)
	})
}

// flagType is a helper for PrintDefaults to guess the type name. Needs improvement for non-builtins.
func flagType(f *Flag) (name string, hasArgument bool) {
	if f.IsBool {
		return "", false // Booleans don't typically show a type name
	}
	hasArgument = true // Assume others take arguments
	// Basic type guessing
	switch f.Value.(type) {
	case *stringValue:
		name = "string"
	case *stringSliceValue:
		name = "string" // Base type is string, PrintDefaults adds "..."
	// case *intValue: name = "int" // Add if implementing int
	default:
		name = "value" // Generic placeholder
	}
	return
}

// isNumeric checks if a string is purely numeric (potentially negative).
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	// Allow negative sign only at the start
	if s[0] == '-' {
		if len(s) == 1 {
			return false
		} // Just "-" is not numeric
		s = s[1:]
		if s == "" {
			return false
		} // Just "-" is not numeric
	}
	// Check remaining characters are digits
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Helper to get map keys for logging set contents
func mapsKeys[M ~map[K]V, K comparable, V any](m M) []K {
	r := make([]K, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	// Sort for consistent log output
	sort.Slice(r, func(i, j int) bool {
		// Assuming K is string or comparable type convertible to string
		return fmt.Sprint(r[i]) < fmt.Sprint(r[j])
	})
	return r
}
