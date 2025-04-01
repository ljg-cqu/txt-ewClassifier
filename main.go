/*
   TextLinguisticAnalyzer: A Go program for linguistic analysis of English text files.

   Features:
   - Automatically outputs results to a directory named after the input file name (without extension).
   - Ensures `_all.txt` includes only deduplicated words from the category files.
   - Enforces exclusivity so every word appears in only one category file.
   - Words in `_all.txt` appear in decreasing order of global frequency.
   - Uses the input file name as a prefix for all output files.
   - Handles slash-separated words as separate entries (e.g., "Writer/Copywriter").
   - Filters non-English text, deduplicates entries, and sorts by frequency.
   - Categorizes words by parts of speech and linguistic function.
   - Capitalizes the first letter of each word for readability.
   - Optionally waits for user confirmation or delays program exit.

   Workflow:
   - User selects input text file via GUI dialog.
   - Text is processed using the Prose NLP library for POS tagging.
   - Results are written to output files in the directory named after the input file name.
   - User can confirm the results via an "OK" button, or the program will delay for 3 seconds before exiting.
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jdkato/prose/v2"
	"github.com/sqweek/dialog"
)

// Helper function to check if text contains only valid English characters
func isEnglishText(text string) bool {
	for _, r := range text {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '/' {
			return false
		}
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

// Splits slash-separated words (e.g., "Writer/Copywriter") into individual words
func splitSlashSeparatedWords(text string) []string {
	parts := strings.Split(text, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
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

// Converts frequency map to a slice sorted by frequency (descending order)
func sortByFrequency(counts map[string]int) []string {
	type itemFrequency struct {
		Item      string
		Frequency int
	}
	var items []itemFrequency
	for item, freq := range counts {
		items = append(items, itemFrequency{Item: item, Frequency: freq})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Frequency > items[j].Frequency
	})
	var sortedItems []string
	for _, entry := range items {
		sortedItems = append(sortedItems, entry.Item)
	}
	return sortedItems
}

// Corrected categorizeText function without the unused prioritizedCategories variable

func categorizeText(inputFile string) error {
	baseFileName := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
	outputDir := baseFileName

	// Create the output directory
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Open the input file
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %v", err)
	}
	defer file.Close()

	// Read the content
	scanner := bufio.NewScanner(file)
	var content string
	for scanner.Scan() {
		content += scanner.Text() + " "
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input file: %v", err)
	}

	// Create NLP document
	doc, err := prose.NewDocument(content)
	if err != nil {
		return fmt.Errorf("error creating Prose document: %v", err)
	}

	// Define output files
	categoryFiles := map[string]string{
		"Nouns":      baseFileName + "_Nouns.txt",
		"Verbs":      baseFileName + "_Verbs.txt",
		"Adjectives": baseFileName + "_Adjectives.txt",
		"Adverbs":    baseFileName + "_Adverbs.txt",
		"OtherWords": baseFileName + "_OtherWords.txt",
	}

	categorizedWords := make(map[string]string) // Keeps track of which category a word belongs to
	categorizedContent := make(map[string][]string)
	allWords := make(map[string]int) // Deduplicated global count for all words

	// Process tokens for classification
	for _, tok := range doc.Tokens() {
		text := strings.ToLower(tok.Text)

		// Process slash-separated words
		wordParts := splitSlashSeparatedWords(text)
		for _, part := range wordParts {
			if isEnglishText(part) {
				allWords[part]++
				if _, alreadyCategorized := categorizedWords[part]; !alreadyCategorized {
					switch tok.Tag {
					case "NN", "NNS", "NNP", "NNPS":
						categorizedWords[part] = "Nouns"
						categorizedContent["Nouns"] = append(categorizedContent["Nouns"], part)
					case "VB", "VBD", "VBP", "VBZ", "VBG":
						categorizedWords[part] = "Verbs"
						categorizedContent["Verbs"] = append(categorizedContent["Verbs"], part)
					case "JJ", "JJR", "JJS":
						categorizedWords[part] = "Adjectives"
						categorizedContent["Adjectives"] = append(categorizedContent["Adjectives"], part)
					case "RB", "RBR", "RBS":
						categorizedWords[part] = "Adverbs"
						categorizedContent["Adverbs"] = append(categorizedContent["Adverbs"], part)
					default:
						categorizedWords[part] = "OtherWords"
						categorizedContent["OtherWords"] = append(categorizedContent["OtherWords"], part)
					}
				}
			}
		}
	}

	// Write categorized content to respective files
	for category, words := range categorizedContent {
		filename := categoryFiles[category]
		filePath := filepath.Join(outputDir, filename)
		file, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create output file for %s: %v", category, err)
		}
		defer file.Close()

		writer := bufio.NewWriter(file)
		countedContent := countFrequencies(words)
		sortedContent := sortByFrequency(countedContent)
		for _, item := range sortedContent {
			writer.WriteString(item + "\n")
		}
		writer.Flush()
	}

	// Write `_AllWords.txt` file
	allFilePath := filepath.Join(outputDir, baseFileName+"_AllWords.txt")
	allFile, err := os.Create(allFilePath)
	if err != nil {
		return fmt.Errorf("failed to create unified all.txt file: %v", err)
	}
	defer allFile.Close()

	allWriter := bufio.NewWriter(allFile)
	sortedAllWords := sortByFrequency(allWords)
	for _, word := range sortedAllWords {
		allWriter.WriteString(capitalizePhrase(word) + "\n")
	}
	allWriter.Flush()

	// Report results
	fmt.Printf("\n===== Analysis Results =====\n")
	fmt.Printf("Total unique words after deduplication: %d\n", len(allWords))
	fmt.Printf("Results written to directory: %s\n", outputDir)

	return nil
}

func main() {
	fmt.Println("Select the input text file:")
	inputFile, err := dialog.File().Title("Select Input File").Filter("Text Files (*.txt)", "txt").Load()
	if err != nil || inputFile == "" {
		fmt.Println("No file selected or error occurred:", err)
		return
	}

	err = categorizeText(inputFile)
	if err != nil {
		fmt.Println("Error during categorization:", err)
		return
	}

	fmt.Println("Text analysis complete.")

	// Wait for user confirmation
	dialog.Message("Analysis complete. Click 'OK' to exit.").Title("Analysis Results").Info()

	// Fallback: Sleep for 3 seconds before exiting (in case the GUI doesn't support dialog input)
	time.Sleep(3 * time.Second)
}
