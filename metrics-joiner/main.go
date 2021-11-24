package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gitlab.com/NebulousLabs/errors"
)

// joinFilesSum will join the srcFile into the destFile.
func joinFilesSum(srcPath, destPath, dateProcessed string) (err error) {
	// Get the source files opened and read.
	srcFile, err := os.OpenFile(srcPath, os.O_RDONLY, 0644)
	if err != nil {
		return errors.AddContext(err, "unable to open source file")
	}
	defer func() {
		closeErr := srcFile.Close()
		if closeErr != nil {
			closeErr = errors.AddContext(err, "unable to close source file")
			err = errors.Compose(err, closeErr)
		}
	}()
	srcContents, err := ioutil.ReadAll(srcFile)
	if err != nil {
		return errors.AddContext(err, "unable to read source file")
	}

	// Get the dest files opened.
	err = os.MkdirAll(filepath.Dir(destPath), 0755)
	if err != nil {
		return errors.AddContext(err, "unable to MkdirAll the destPath")
	}
	destFile, err := os.OpenFile(destPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return errors.AddContext(err, "unable to open dest file")
	}
	defer func() {
		closeErr := destFile.Close()
		if closeErr != nil {
			closeErr = errors.AddContext(err, "unable to close source file")
			err = errors.Compose(err, closeErr)
		}
	}()
	destContents, err := ioutil.ReadAll(destFile)
	if err != nil {
		return errors.AddContext(err, "unable to read dest file")
	}

	// Get the lines for the download files.
	srcLines := strings.Split(string(srcContents), "\n")
	destLines := strings.Split(string(destContents), "\n")
	// Trim the final newlines
	if len(srcLines[len(srcLines)-1]) < 1 {
		srcLines = srcLines[:len(srcLines)-1]
	}
	if len(destLines[len(destLines)-1]) < 1 {
		destLines = destLines[:len(destLines)-1]
	}

	// Filter all lines that have already been processed.
	for len(srcLines) > 0 {
		srcFields := strings.Split(srcLines[0], " ")
		if srcFields[0] <= dateProcessed {
			srcLines = srcLines[1:]
		} else {
			break
		}
	}

	// Build the new lines.
	var newLines []string
	for len(srcLines) > 0 || len(destLines) > 0 {
		// Base case.
		if len(srcLines) == 0 {
			newLines = append(newLines, destLines...)
			break
		}
		if len(destLines) == 0 {
			newLines = append(newLines, srcLines...)
			break
		}

		// We have both lines, figure out which to add, or if they need to be
		// merged.
		srcFields := strings.Split(srcLines[0], " ")
		destFields := strings.Split(destLines[0], " ")
		if srcFields[0] < destFields[0] {
			newLines = append(newLines, srcLines[0])
			srcLines = srcLines[1:]
		} else if destFields[0] < srcFields[0] {
			newLines = append(newLines, destLines[0])
			destLines = destLines[1:]
		} else {
			sN, err := strconv.Atoi(srcFields[1])
			if err != nil {
				return errors.AddContext(err, "unable to convert source field to int")
			}
			dN, err := strconv.Atoi(destFields[1])
			if err != nil {
				return errors.AddContext(err, "unable to convert dest field to int")
			}
			sum := sN + dN
			newLines = append(newLines, srcFields[0] + " " + strconv.Itoa(sum))
			srcLines = srcLines[1:]
			destLines = destLines[1:]
		}
	}
	// Write the new lines to the dest file.
	_, err = destFile.Seek(0, 0)
	if err != nil {
		return errors.AddContext(err, "unable to seek to front of dest file")
	}
	err = destFile.Truncate(0)
	if err != nil {
		return errors.AddContext(err, "unable to set new filesize to zero")
	}
	newDestData := strings.Join(newLines, "\n")
	_, err = destFile.Write([]byte(newDestData))
	if err != nil {
		return errors.AddContext(err, "unable to write to dest file")
	}

	// Build the data for the graph files.
	newXData := make([]string, 0, len(newLines))
	newYData := make([]string, 0, len(newLines))
	for i := 0; i < len(newLines); i++ {
		xy := strings.Split(newLines[i], " ")
		xy[0] = strings.ReplaceAll(xy[0], ".", "-")
		newXData = append(newXData, xy[0])
		newYData = append(newYData, xy[1])
	}
	joinedX := strings.Join(newXData, "', '")
	joinedY := strings.Join(newYData, ", ")

	// Get the name prefix for the variables.
	destBase := filepath.Base(destPath)
	prefix := strings.TrimSuffix(destBase, filepath.Ext(destBase))
	fullFile := "var "+prefix+"X = ['"+joinedX+"'];\nvar "+prefix+"Y = ["+joinedY+"];\n"

	// Open the file and write it.
	var pathComponents []string
	temp := destPath
	for temp != "." {
		pathComponents = append([]string{filepath.Base(temp)}, pathComponents...)
		temp = filepath.Dir(temp)
	}
	pathComponents[1] = "graphs"
	graphPath := filepath.Join(pathComponents...)
	graphFilename := strings.TrimSuffix(graphPath, filepath.Ext(graphPath))+".js"
	err = os.MkdirAll(filepath.Dir(graphFilename), 0755)
	if err != nil {
		return errors.AddContext(err, "unable to create dirs for graph file")
	}
	graphFile, err := os.OpenFile(graphFilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return errors.AddContext(err, "unable to open the graph file")
	}
	_, err = graphFile.Write([]byte(fullFile))
	if err != nil {
		return errors.AddContext(err, "unable to write to the graph file")
	}
	err = graphFile.Close()
	if err != nil {
		return errors.AddContext(err, "unable to close the graph file")
	}
	return nil
}

// joinUniques will combine a source file and a dest file that list a set of
// unique elements separated by day 
func joinUniques(srcPath, destPath, dateProcessed string) (err error) {
	// Get the source files opened and read.
	srcFile, err := os.OpenFile(srcPath, os.O_RDONLY, 0644)
	if os.IsNotExist(err) {
		// joinUniques is run for every top app on every server, but not every
		// server has metrics for every top app. When this happens, we can get a
		// DNE on the file, which means there is nothing to do and we can exit
		// with a success. The servers that do contain this file will perform
		// all the required steps.
		return nil
	}
	if err != nil {
		return errors.AddContext(err, "unable to open source file")
	}
	defer func() {
		closeErr := srcFile.Close()
		if closeErr != nil {
			closeErr = errors.AddContext(err, "unable to close source file")
			err = errors.Compose(err, closeErr)
		}
	}()
	srcContents, err := ioutil.ReadAll(srcFile)
	if err != nil {
		return errors.AddContext(err, "unable to read source file")
	}

	// Get the dest files opened.
	err = os.MkdirAll(filepath.Dir(destPath), 0755)
	if err != nil {
		return errors.AddContext(err, "unable to MkdirAll the destPath")
	}
	destFile, err := os.OpenFile(destPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return errors.AddContext(err, "unable to open dest file")
	}
	defer func() {
		closeErr := destFile.Close()
		if closeErr != nil {
			closeErr = errors.AddContext(err, "unable to close source file")
			err = errors.Compose(err, closeErr)
		}
	}()
	destContents, err := ioutil.ReadAll(destFile)
	if err != nil {
		return errors.AddContext(err, "unable to read dest file")
	}

	// Split the file contents into their daily sections.
	srcSections := bytes.Split(srcContents, []byte{'\n', '!', '\n'})
	destSections := bytes.Split(destContents, []byte{'\n', '!', '\n'})

	// Check for empty sections. For each src and dest, first check if the first
	// section is empty, then check if the last section is empty. Need to wrap
	// each step in a zero check because after we trim the first section it
	// could be empty again.
	if len(srcSections) > 0 {
		if len(srcSections[0]) < 4 {
			srcSections = srcSections[1:]
		}
	}
	if len(srcSections) > 0 {
		if len(srcSections[len(srcSections)-1]) < 4 {
			srcSections = srcSections[:len(srcSections)-1]
		}
	}
	if len(destSections) > 0 {
		if len(destSections[0]) < 4 {
			destSections = destSections[1:]
		}
	}
	if len(destSections) > 0 {
		if len(destSections[len(destSections)-1]) < 4 {
			destSections = destSections[:len(destSections)-1]
		}
	}

	// Merge the set of sections together, building the datapoints for the
	// graphs, and building the new file data.
	var newSections [][]byte
	var xDataDaily []string
	var yDataDaily []string
	var xDataMonthly []string
	var yDataMonthly []string
	var xDataChurn []string
	var yDataChurn []string
	var currentDay, prevDay []byte
	currentMonthIPs := make(map[string]struct{})
	var prevMonthIPs map[string]struct{}
	for len(srcSections) > 0 || len(destSections) > 0 {
		// Merge the two sections into one section.
		var mergedSection []byte
		if len(srcSections) == 0 {
			currentDay = destSections[0][:10]
			mergedSection = destSections[0]
			destSections = destSections[1:]
		} else if len(destSections) == 0 {
			currentDay = srcSections[0][:10]
			mergedSection = srcSections[0]
			srcSections = srcSections[1:]
		} else {
			// The first 10 characters of the section are the date, check if the
			// dates don't match.
			cmp := bytes.Compare(srcSections[0][:10], destSections[0][:10])
			if cmp < 0 {
				currentDay = srcSections[0][:10]
				mergedSection = srcSections[0]
				srcSections = srcSections[1:]
			} else if cmp > 0 {
				currentDay = destSections[0][:10]
				mergedSection = destSections[0]
				destSections = destSections[1:]
			} else {
				// The two sections need to be merged into one. We want to
				// ignore the date line in the second section.
				currentDay = destSections[0][:10]
				mergedSection = append(srcSections[0], destSections[0][11:]...)
				srcSections = srcSections[1:]
				destSections = destSections[1:]
			}
		}

		// Check if we need to cycle the month.
		if prevDay != nil && bytes.Compare(prevDay[:7], currentDay[:7]) < 0 {
			// Calculate the churn percentage, which is the number of IPs that
			// appeared in the previous month but not the current month. We can
			// only do this if there is a previous month.
			if prevMonthIPs != nil {
				var churnedIPs int
				for ip, _ := range prevMonthIPs {
					_, exists := currentMonthIPs[ip]
					if !exists {
						churnedIPs++
					}
				}
				prevMonthBytes := make([]byte, 7)
				copy(prevMonthBytes, prevDay)
				prevMonthBytes[4] = '-'
				xDataChurn = append(xDataChurn, string(prevMonthBytes))
				churn := float64(churnedIPs) / float64(len(prevMonthIPs))
				yDataChurn = append(yDataChurn, strconv.FormatFloat(churn*100, 'f', 2, 64))
			}

			// Create a new data point for the number of IPs in the current
			// month.
			currentMonthBytes := make([]byte, 7)
			copy(currentMonthBytes, currentDay)
			currentMonthBytes[4] = '-'
			xDataMonthly = append(xDataMonthly, string(currentMonthBytes))
			yDataMonthly = append(yDataMonthly, strconv.Itoa(len(currentMonthIPs)))

			// Rotate the maps that track the IPs.
			prevMonthIPs = currentMonthIPs
			currentMonthIPs = make(map[string]struct{})
		}
		prevDay = currentDay

		// We now have the merged section, which may or may not contain
		// duplicate IPs. Parse it to fill out the IP maps.
		currentDayIPs := make(map[string]struct{})
		mergedLines := bytes.Split(mergedSection[11:], []byte{'\n'})
		for i := 0; i < len(mergedLines); i++ {
			currentDayIPs[string(mergedLines[i])] = struct{}{}
			currentMonthIPs[string(mergedLines[i])] = struct{}{}
		}

		// Fill out the daily data for the graph.
		currentDayBytes := make([]byte, 10)
		copy(currentDayBytes, currentDay)
		currentDayBytes[4] = '-'
		currentDayBytes[7] = '-'
		xDataDaily = append(xDataDaily, string(currentDayBytes))
		yDataDaily = append(yDataDaily, strconv.Itoa(len(currentDayIPs)))

		// Create the new section from the current day IP map.
		finalLines := make([][]byte, len(currentDayIPs)+1)
		finalLines[0] = currentDay
		i := 1
		for ip, _ := range currentDayIPs {
			finalLines[i] = []byte(ip)
			i++
		}

		// Join the merged lines into the new section and append it to the list
		// of sections.
		newSections = append(newSections, bytes.Join(finalLines, []byte{'\n'}))
	}

	// All data collected. Write the new sections to the dest file.
	_, err = destFile.Seek(0, 0)
	if err != nil {
		return errors.AddContext(err, "unable to seek to front of dest file")
	}
	err = destFile.Truncate(0)
	if err != nil {
		return errors.AddContext(err, "unable to set new filesize to zero")
	}
	newDestData := bytes.Join(newSections, []byte{'\n', '!', '\n'})
	_, err = destFile.Write([]byte(newDestData))
	if err != nil {
		return errors.AddContext(err, "unable to write to dest file")
	}
	err = destFile.Close()
	if err != nil {
		return errors.AddContext(err, "unable to close dest file")
	}

	// Write the graph data to the graph file.
	xDataDailyJoined := strings.Join(xDataDaily, "','")
	yDataDailyJoined := strings.Join(yDataDaily, ",")
	xDataMonthlyJoined := strings.Join(xDataMonthly, "','")
	yDataMonthlyJoined := strings.Join(yDataMonthly, ",")
	xDataChurnJoined := strings.Join(xDataChurn, "','")
	yDataChurnJoined := strings.Join(yDataChurn, ",")
	fullFile := "var xDailyIPs = ['" + string(xDataDailyJoined) + "'];\n"
	fullFile += "var yDailyIPs = [" + string(yDataDailyJoined) + "];\n"
	fullFile += "var xMonthlyIPs = ['" + string(xDataMonthlyJoined) + "'];\n"
	fullFile += "var yMonthlyIPs = [" + string(yDataMonthlyJoined) + "];\n"
	fullFile += "var xChurn = ['" + string(xDataChurnJoined) + "'];\n"
	fullFile += "var yChurn = [" + string(yDataChurnJoined) + "];\n"
	var pathComponents []string
	temp := destPath
	for temp != "." {
		pathComponents = append([]string{filepath.Base(temp)}, pathComponents...)
		temp = filepath.Dir(temp)
	}
	pathComponents[1] = "graphs"
	pathComponents[len(pathComponenets)-1] = "ipData.js"
	ipFilename := filepath.Join(pathComponents...)
	ipFilename := filepath.Join(filepath.Dir(destPath), "ipData.js")
	ipFile, err := os.OpenFile(ipFilename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return errors.AddContext(err, "unable to open ip file")
	}
	_, err = ipFile.Write([]byte(fullFile))
	if err != nil {
		return errors.AddContext(err, "unable to write to ipFile")
	}
	err = ipFile.Close()
	if err != nil {
		return errors.AddContext(err, "unable to close the ip file")
	}
	return nil
}

func main() {
	// Check the args are right.
	if len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Println("improper use, please provide the source directory of the directory being merged")
		fmt.Println("Usage: ./joiner [server] [app-dir]")
		fmt.Println("Usage: ./joiner [server] [app-dir] ips")
		fmt.Println(os.Args)
		return
	}

	// Try joining the download file.
	if len(os.Args) == 3 {
		downloadsSrcPath := filepath.Join("combined-metrics", "tmp", os.Args[2], "downloads.txt")
		downloadsDestPath := filepath.Join("combined-metrics", "joined-data", os.Args[2], "downloads.txt")
		err := joinFilesSum(downloadsSrcPath, downloadsDestPath, os.Args[1])
		if err != nil {
			fmt.Printf("Unable to join files %v and %v: %v\n", downloadsSrcPath, downloadsDestPath, err)
			return
		}

		// Try joining the upload file.
		uploadsSrcPath := filepath.Join("combined-metrics", "tmp", os.Args[2], "uploads.txt")
		uploadsDestPath := filepath.Join("combined-metrics", "joined-data", os.Args[2], "uploads.txt")
		err = joinFilesSum(uploadsSrcPath, uploadsDestPath, os.Args[1])
		if err != nil {
			fmt.Printf("Unable to join files %v and %v: %v\n", uploadsSrcPath, uploadsDestPath, err)
			return
		}
	} else if len(os.Args) == 4 {
		// Try joining the ip-data
		ipDataSrcPath := filepath.Join("combined-metrics", "tmp", os.Args[2], "ips.txt")
		ipDataDestPath := filepath.Join("combined-metrics", "joined-data", os.Args[2], "ips.txt")
		err := joinUniques(ipDataSrcPath, ipDataDestPath, os.Args[1])
		if err != nil {
			fmt.Printf("Unable to unique-join files %v and %v: %v\n", ipDataSrcPath, ipDataDestPath, err)
			return
		}
	}
}
