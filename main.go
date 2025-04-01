/*
	TextLinguisticAnalyzer: A Go program for linguistic analysis of English text files.

Features:

Categorizes words by parts of speech: nouns, verbs, adjectives, adverbs

Extracts and identifies phrases: noun phrases, verb phrases, idioms, slang, common phrases

# Filters out non-English text automatically

# Deduplicates entries and sorts by frequency of occurrence

# Capitalizes first letter of each word for readability

# Outputs results to separate category files

Workflow:

# User selects input text file and output directory via GUI dialog

# Program processes text using the prose NLP library for POS tagging

# Words and phrases are categorized based on linguistic properties

# Program counts frequency, deduplicates, and sorts items

Results are written to separate files (e.g., Nouns.txt, Verbs.txt, Idioms.txt)

Each output file contains items sorted by occurrence frequency
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/jdkato/prose/v2"
	"github.com/sqweek/dialog"
)

// Helper function to check if text contains only valid English characters
func isEnglishText(text string) bool {
	for _, r := range text {
		// Only allow Latin characters, spaces, and hyphens
		if !unicode.IsLetter(r) && r != ' ' && r != '-' {
			return false
		}
		// Ensure the character is part of the Latin script range
		if !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return true
}

// Capitalizes the first letter of a word or phrase
func capitalizePhrase(phrase string) string {
	words := strings.Fields(phrase)
	for i, word := range words {
		words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
	}
	return strings.Join(words, " ")
}

// Deduplicates and counts frequencies of items
func countFrequencies(content []string) map[string]int {
	counts := make(map[string]int)
	for _, item := range content {
		capitalizedItem := capitalizePhrase(item)
		counts[capitalizedItem]++
	}
	return counts
}

// Converts frequency map to sorted slice (only items, sorted by frequency)
func sortByFrequency(counts map[string]int) []string {
	type itemFrequency struct {
		Item      string
		Frequency int
	}
	var items []itemFrequency
	for item, freq := range counts {
		items = append(items, itemFrequency{Item: item, Frequency: freq})
	}
	// Sort by frequency in descending order
	sort.Slice(items, func(i, j int) bool {
		return items[i].Frequency > items[j].Frequency
	})
	// Return only the items in sorted order
	var sortedItems []string
	for _, entry := range items {
		sortedItems = append(sortedItems, entry.Item)
	}
	return sortedItems
}

// Categorizes text based on linguistic features and skips non-English characters
func categorizeText(inputFile string, outputDir string) error {
	// Open the input file for reading
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %v", err)
	}
	defer file.Close()

	// Read the content of the input file
	scanner := bufio.NewScanner(file)
	var content string
	for scanner.Scan() {
		content += scanner.Text() + " "
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input file: %v", err)
	}

	// Create a Prose document for NLP processing
	doc, err := prose.NewDocument(content)
	if err != nil {
		return fmt.Errorf("error creating Prose document: %v", err)
	}

	// Mapping categories to filenames
	categoryFiles := map[string]string{
		"Nouns":         "Nouns.txt",
		"Verbs":         "Verbs.txt",
		"Adjectives":    "Adjectives.txt",
		"Adverbs":       "Adverbs.txt",
		"NounPhrases":   "NounPhrases.txt",
		"VerbPhrases":   "VerbPhrases.txt",
		"Idioms":        "Idioms.txt",
		"Slang":         "Slang.txt",
		"CommonPhrases": "CommonPhrases.txt",
		"OtherWords":    "OtherWords.txt",
	}

	// Sample datasets for idioms, slang, and common phrases
	idioms := []string{"piece of cake", "spill the beans", "hit the nail on the head"}
	slang := []string{"cool", "kick the bucket", "off the hook"}
	commonPhrases := []string{"good morning", "how are you", "have a nice day"}

	// Prepare storage for categorized content
	results := make(map[string][]string)

	// Process each token in the document for single-word POS tagging
	for _, tok := range doc.Tokens() {
		text := strings.ToLower(tok.Text) // Normalize input text to lowercase

		// Skip non-English text (e.g., Chinese characters)
		if isEnglishText(text) {
			switch tok.Tag {
			case "NN", "NNS", "NNP", "NNPS":
				results["Nouns"] = append(results["Nouns"], text)
			case "VB", "VBD", "VBP", "VBZ", "VBG":
				results["Verbs"] = append(results["Verbs"], text)
			case "JJ", "JJR", "JJS":
				results["Adjectives"] = append(results["Adjectives"], text)
			case "RB", "RBR", "RBS":
				results["Adverbs"] = append(results["Adverbs"], text)
			default:
				results["OtherWords"] = append(results["OtherWords"], text)
			}
		}
	}

	// Categorize idioms, slang, and common phrases
	for _, tok := range doc.Tokens() {
		text := strings.ToLower(tok.Text)

		// Skip non-English text
		if isEnglishText(text) {
			if matchesPhraseList(text, idioms) {
				results["Idioms"] = append(results["Idioms"], text)
			} else if matchesPhraseList(text, slang) {
				results["Slang"] = append(results["Slang"], text)
			} else if matchesPhraseList(text, commonPhrases) {
				results["CommonPhrases"] = append(results["CommonPhrases"], text)
			}
		}
	}

	// Extract noun phrases based on POS patterns
	for _, chunk := range doc.Entities() {
		if isEnglishText(chunk.Text) {
			if chunk.Label == "NP" {
				results["NounPhrases"] = append(results["NounPhrases"], chunk.Text)
			}
		}
	}

	// Extract verb phrases
	for _, tok := range doc.Tokens() {
		if isEnglishText(tok.Text) && strings.HasPrefix(tok.Tag, "VB") {
			results["VerbPhrases"] = append(results["VerbPhrases"], tok.Text)
		}
	}

	// Write output files for each linguistic category, sorted by frequency
	for category, filename := range categoryFiles {
		filePath := filepath.Join(outputDir, filename)
		file, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create output file for %s: %v", category, err)
		}
		defer file.Close()

		writer := bufio.NewWriter(file)
		countedContent := countFrequencies(results[category])
		sortedContent := sortByFrequency(countedContent)
		// Write items to the file without frequency numbers
		for _, item := range sortedContent {
			writer.WriteString(item + "\n")
		}
		writer.Flush()
	}

	return nil
}

// Matches phrases from a list
func matchesPhraseList(phrase string, list []string) bool {
	for _, item := range list {
		if strings.EqualFold(item, phrase) {
			return true
		}
	}
	return false
}

func main() {
	// Let user select input file via dialog
	fmt.Println("Select the input text file:")
	inputFile, err := dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
	if err != nil || inputFile == "" {
		fmt.Println("No file selected or error occurred:", err)
		return
	}

	// Let user select output directory via dialog
	fmt.Println("Select the output directory:")
	outputDir, err := dialog.Directory().Title("Select Output Directory").Browse()
	if err != nil || outputDir == "" {
		fmt.Println("No directory selected or error occurred:", err)
		return
	}

	// Perform categorization
	err = categorizeText(inputFile, outputDir)
	if err != nil {
		fmt.Println("Error during categorization:", err)
		return
	}

	fmt.Println("Text has been categorized and written to output files.")
}
