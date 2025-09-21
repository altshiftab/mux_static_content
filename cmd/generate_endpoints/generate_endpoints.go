package main

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	motmedelContext "github.com/Motmedel/utils_go/pkg/context"
	motmedelUtilsEnv "github.com/Motmedel/utils_go/pkg/env"
	motmedelErrors "github.com/Motmedel/utils_go/pkg/errors"
	motmedelHttpContext "github.com/Motmedel/utils_go/pkg/http/context"
	motmedelHttpErrors "github.com/Motmedel/utils_go/pkg/http/errors"
	motmedelHttpLog "github.com/Motmedel/utils_go/pkg/http/log"
	"github.com/Motmedel/utils_go/pkg/http/mux/types/endpoint_specification"
	"github.com/Motmedel/utils_go/pkg/http/mux/utils/generate"
	motmedelHttpUtils "github.com/Motmedel/utils_go/pkg/http/utils"
	motmedelLog "github.com/Motmedel/utils_go/pkg/log"
	motmedelContextLogger "github.com/Motmedel/utils_go/pkg/log/context_logger"
	errorLogger "github.com/Motmedel/utils_go/pkg/log/error_logger"
	"github.com/vphpersson/code_generation_go/pkg/code_generation"
)

func main() {
	logger := errorLogger.Logger{
		Logger: motmedelContextLogger.New(
			slog.NewJSONHandler(os.Stderr, nil),
			&motmedelLog.ErrorContextExtractor{},
			&motmedelHttpLog.HttpContextExtractor{},
		),
	}
	slog.SetDefault(logger.Logger)

	var path string
	flag.StringVar(&path, "path", "", "path to generate code from")

	var packageName string
	flag.StringVar(
		&packageName,
		"package-name",
		motmedelUtilsEnv.GetEnvWithDefault("GOPACKAGE", "main"),
		"The name of the package in the output.",
	)

	var variableName string
	flag.StringVar(&variableName, "variable", "x", "The name of the variable in the output.")

	var addPathComment bool
	flag.BoolVar(&addPathComment, "add-path-comment", false, "Add a comment of the path.")

	var private bool
	flag.BoolVar(
		&private,
		"private",
		false,
		"Whether the generated static content is private. Affects Cache-Control.",
	)

	flag.Parse()

	if path == "" {
		logger.FatalWithExitingMessage("Empty path.", nil)
	}

	var specifications []*endpoint_specification.EndpointSpecification
	resultingPaths := []string{path}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		ctxWithHttp := motmedelHttpContext.WithHttpContext(context.Background())
		httpClient := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if request == nil {
					return motmedelErrors.NewWithTrace(motmedelHttpErrors.ErrNilHttpRequest)
				}

				requestUrl := request.URL
				if requestUrl == nil {
					return motmedelErrors.NewWithTrace(motmedelHttpErrors.ErrNilHttpRequestUrl)
				}

				resultingPaths = append(resultingPaths, requestUrl.String())

				return nil
			},
		}
		response, body, err := motmedelHttpUtils.Fetch(ctxWithHttp, path, httpClient, nil)
		if err != nil {
			logger.ErrorContext(
				motmedelContext.WithErrorContextValue(
					ctxWithHttp,
					motmedelErrors.New(fmt.Errorf("fetch: %w", err), path),
				),
				"An error occurred when fetching. Exiting.",
			)
			os.Exit(1)
		}
		if response == nil {
			logger.ErrorContext(
				motmedelContext.WithErrorContextValue(
					ctxWithHttp,
					motmedelErrors.New(motmedelHttpErrors.ErrNilHttpResponse, path),
				),
				"The HTTP response is nil. Exiting.",
			)
			os.Exit(1)
		}

		bytesReader := bytes.NewReader(body)
		size := int64(len(body))
		zipReader, err := zip.NewReader(bytesReader, size)
		if err != nil {
			logger.FatalWithExitingMessage(
				"An error occurred when creating a zip reader. Does the body constitute a Zip file?",
				fmt.Errorf("zip new reader: %w", err),
				bytesReader, size,
			)
		}

		specifications, err = generate.EndpointSpecificationsFromZip(zipReader, true, private)
		if err != nil {
			logger.FatalWithExitingMessage(
				"An error occurred when creating endpoint specifications from zip data.",
				fmt.Errorf("endpoint specifications from zip: %w", err),
				zipReader,
			)
		}
	} else {
		var err error
		specifications, err = generate.EndpointSpecificationsFromDirectory(path, true, private)
		if err != nil {
			logger.FatalWithExitingMessage(
				"An error occurred when creating endpoint specifications from a directory.",
				fmt.Errorf("endpoint specifications from directory: %w", err),
				path,
			)
		}
	}

	output, err := code_generation.GetGeneratedFileContents(
		specifications,
		packageName,
		"github.com/altshiftab/mux_static_content/cmd/generate_endpoints",
		variableName,
		nil,
	)
	if err != nil {
		logger.FatalWithExitingMessage(
			"An error occurred when obtaining the generated file contents.",
			fmt.Errorf("generated file contents: %w", err),
			specifications, packageName, variableName,
		)
	}

	if addPathComment {
		var pathOutput []byte
		for _, resultingPath := range resultingPaths {
			pathOutput = append(pathOutput, []byte(fmt.Sprintf("// Path: %s\n", resultingPath))...)
		}

		output = append(pathOutput, output...)
	}

	if fileName := code_generation.GetGeneratedFilename(); fileName != "" {
		if err := os.WriteFile(fileName, output, 0644); err != nil {
			logger.FatalWithExitingMessage(
				"An error occurred when writing the file.",
				motmedelErrors.New(fmt.Errorf("os write file: %w", err), fileName, output),
			)
		}
	} else {
		fmt.Println(string(output))
	}
}
